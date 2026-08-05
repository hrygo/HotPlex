package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
)

// newRuntimeCmd builds `hotplex runtime` — operator fence inspection and
// decisions (#877). All subcommands call the Admin API over HTTP; the CLI
// never opens the database for fence reads or writes (spec §5.7).
func newRuntimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Runtime operations: inspect and resolve fenced executions",
	}
	cmd.AddCommand(newRuntimeFencesCmd())
	return cmd
}

func newRuntimeFencesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fences",
		Short: "List and decide fenced executions (via Admin API)",
	}
	cmd.AddCommand(newFencesListCmd(), newFencesActionCmd("resolve"), newFencesActionCmd("abandon"))
	return cmd
}

// --- Wire DTOs (mirror of admin.FenceListItem; the CLI owns its decoding) ---

type cliFenceItem struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id"`
	DeliveryStatus string `json:"delivery_status"`
	RuntimeStatus  string `json:"runtime_status"`
	FenceReason    string `json:"fence_reason"`
	FenceVersion   int64  `json:"fence_version"`
	FenceCreatedAt *int64 `json:"fence_created_at,omitempty"`
	UpdatedAt      int64  `json:"updated_at"`
}

type cliFenceListResponse struct {
	Fences []cliFenceItem `json:"fences"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// --- Admin API client ---

type fenceAdminClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// newFenceAdminClient resolves the gateway base URL and admin token from the
// local config (same host trust model as the rest of the CLI). The token can
// be overridden with HOTPLEX_ADMIN_TOKEN for split-admin deployments.
func newFenceAdminClient(configPath string) (*fenceAdminClient, error) {
	absPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	loadEnvFile(filepath.Dir(absPath))
	cfg, err := config.Load(absPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	token := os.Getenv("HOTPLEX_ADMIN_TOKEN")
	if token == "" {
		if len(cfg.Admin.Tokens) == 0 {
			return nil, errors.New("no admin token: set admin.tokens in config or HOTPLEX_ADMIN_TOKEN")
		}
		token = cfg.Admin.Tokens[0]
	}

	addr := cfg.Gateway.Addr
	if addr == "" {
		addr = ":8888"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	scheme := "http"
	if cfg.Security.TLSEnabled {
		scheme = "https"
	}

	return &fenceAdminClient{
		baseURL: scheme + "://" + addr,
		token:   token,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local self-signed certs, same trust model as `hotplex status`
			},
		},
	}, nil
}

func (c *fenceAdminClient) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// fenceAPIError is the AppError wire shape returned by the Admin API.
type fenceAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *fenceAdminClient) listFences(ctx context.Context, sessionID string, limit int) (*cliFenceListResponse, error) {
	q := url.Values{}
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/admin/executions/fences"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("call admin API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, describeFenceAPIError(resp)
	}
	var out cliFenceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode fence list: %w", err)
	}
	return &out, nil
}

func (c *fenceAdminClient) fenceAction(ctx context.Context, executionID, decision string, fenceVersion int64, reason, evidenceRef string) (*cliFenceItem, error) {
	payload, err := json.Marshal(map[string]any{
		"decision":               decision,
		"expected_fence_version": fenceVersion,
		"reason":                 reason,
		"evidence_ref":           evidenceRef,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/admin/executions/"+url.PathEscape(executionID)+"/fence-action", payload)
	if err != nil {
		return nil, fmt.Errorf("call admin API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, describeFenceAPIError(resp)
	}
	var out struct {
		Decision  string       `json:"decision"`
		Execution cliFenceItem `json:"execution"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode fence action response: %w", err)
	}
	return &out.Execution, nil
}

// describeFenceAPIError turns a non-200 into an operator-actionable error.
// 409 explicitly asks for re-inspection — the CLI never auto-retries a
// non-idempotent operator decision (#877 spec §8.3).
func describeFenceAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var apiErr fenceAPIError
	_ = json.Unmarshal(raw, &apiErr) // best-effort; body may be plain text
	if apiErr.Code == "" {
		apiErr.Code = http.StatusText(resp.StatusCode)
		apiErr.Message = strings.TrimSpace(string(raw))
	}
	switch resp.StatusCode {
	case http.StatusConflict:
		return fmt.Errorf("%s: %s — re-inspect with `hotplex runtime fences list` and retry deliberately (no automatic retry)", apiErr.Code, apiErr.Message)
	case http.StatusNotFound:
		return fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
	default:
		return fmt.Errorf("admin API %d %s: %s", resp.StatusCode, apiErr.Code, apiErr.Message)
	}
}

// --- Commands ---

func newFencesListCmd() *cobra.Command {
	var (
		configPath string
		sessionID  string
		jsonOutput bool
		limit      int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List fenced executions blocking fresh input",
		Example: `  hotplex runtime fences list
  hotplex runtime fences list --session-id sess-1 --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newFenceAdminClient(configPath)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			out, err := client.listFences(ctx, sessionID, limit)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(out)
			}
			if len(out.Fences) == 0 {
				fmt.Println("No fenced executions.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "EXECUTION\tSESSION\tRUNTIME\tREASON\tFENCE VERSION\tFENCED AT")
			for _, f := range out.Fences {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
					shortID(f.ExecutionID), shortID(f.SessionID), f.RuntimeStatus,
					f.FenceReason, f.FenceVersion, formatFenceTime(f.FenceCreatedAt))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Printf("\nDecide with: hotplex runtime fences resolve|abandon <execution-id> --fence-version <n> --reason \"...\" --confirm\n")
			return nil
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&sessionID, "session-id", "", "filter by session ID")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (default 100, max 500)")
	return cmd
}

func newFencesActionCmd(decision string) *cobra.Command {
	var (
		configPath  string
		fenceVer    int64
		reason      string
		evidenceRef string
		confirm     bool
	)
	short := "Resolve a fence: clear it, keep runtime=unknown, and unblock the session"
	if decision == "abandon" {
		short = "Abandon a fenced execution: fail it with OPERATOR_ABANDONED and unblock the session"
	}
	cmd := &cobra.Command{
		Use:   decision + " <execution-id>",
		Short: short,
		Long: short + ".\n\n" +
			"The action is conditional on --fence-version; a concurrent operator or a gateway\n" +
			"restart between inspect and action yields 409 FENCE_CONFLICT. On conflict this\n" +
			"command exits non-zero and never retries automatically.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID := strings.TrimSpace(args[0])
			if executionID == "" {
				return errors.New("execution-id is required")
			}
			if !confirm {
				return fmt.Errorf("%s is irreversible for this fence; re-run with --confirm", decision)
			}
			if fenceVer < 0 {
				return errors.New("--fence-version must be >= 0")
			}
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required (1-512 chars)")
			}

			client, err := newFenceAdminClient(configPath)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			updated, err := client.fenceAction(ctx, executionID, decision, fenceVer, reason, evidenceRef)
			if err != nil {
				return err
			}
			fmt.Printf("%s applied: execution=%s session=%s runtime=%s fence_version=%d\n",
				decision, updated.ExecutionID, updated.SessionID, updated.RuntimeStatus, updated.FenceVersion)
			return nil
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().Int64Var(&fenceVer, "fence-version", -1, "expected fence version from `fences list` (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "operator justification, 1-512 chars (required)")
	cmd.Flags().StringVar(&evidenceRef, "evidence-ref", "", "evidence pointer, e.g. ticket or run ID (max 256 chars)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required to apply the decision")
	return cmd
}

func formatFenceTime(ms *int64) string {
	if ms == nil || *ms == 0 {
		return "-"
	}
	return time.UnixMilli(*ms).Format("2006-01-02 15:04:05")
}
