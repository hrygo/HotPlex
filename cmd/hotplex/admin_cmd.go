package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// newAdminCmd builds the `hotplex admin` subcommand for user/account management.
func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "用户与账号管理（bootstrap admin 等）",
	}
	cmd.PersistentFlags().String("config", "", "配置文件路径（默认 ~/.hotplex/config.yaml）")

	create := &cobra.Command{
		Use:   "create",
		Short: "创建账号（首个 admin，或后续用户）",
		RunE:  runAdminCreate,
	}
	create.Flags().String("username", "", "用户名（必填）")
	create.Flags().String("password", "", "密码（省略则交互式提示不回显；最少 8 字符）")
	create.Flags().Bool("admin", true, "创建为 admin 角色")
	_ = create.MarkFlagRequired("username")
	cmd.AddCommand(create)
	return cmd
}

func runAdminCreate(cmd *cobra.Command, _ []string) error {
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	isAdmin, _ := cmd.Flags().GetBool("admin")
	configPath, _ := cmd.Flags().GetString("config")

	if password == "" {
		_, _ = fmt.Fprint(os.Stdout, "Enter password: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = string(raw)
	}
	if len(password) < 8 {
		return fmt.Errorf("password too short (min 8 chars)")
	}

	cfg, err := loadConfig(configPath, false)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	store, err := session.NewSQLiteStore(context.Background(), cfg, writeMu)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	defer func() { _ = store.Close() }()

	role := "user"
	if isAdmin {
		role = "admin"
	}
	idp := security.NewLocalAccountProvider(store, security.BcryptCostDefault)
	hash, err := idp.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u := &security.User{ID: uuid.NewString(), Username: username, PasswordHash: hash, Role: role, Status: "active"}
	if err := store.CreateUser(context.Background(), u, time.Now().Unix()); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "created %s user %q (id=%s)\n", role, username, u.ID)
	return nil
}
