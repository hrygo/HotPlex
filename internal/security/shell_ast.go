package security

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

// ========================================
// Shell AST Parsing
// ========================================

// ShellASTNode represents a node in the shell command abstract syntax tree.
type ShellASTNode struct {
	// Type is the type of the node (e.g., "command", "arg", "pipeline", "redirect").
	Type NodeType

	// Value is the literal value (for literals).
	Value string

	// Name is the command name (for commands).
	Name string

	// Args are the command arguments.
	Args []*ShellASTNode

	// Children are child nodes.
	Children []*ShellASTNode

	// Redirects are I/O redirections.
	Redirects []*Redirect

	// Position in the original input.
	StartPos int
	EndPos   int
}

// NodeType represents the type of a shell AST node.
type NodeType string

const (
	// NodeTypeCommand represents a command.
	NodeTypeCommand NodeType = "command"
	// NodeTypeArg represents an argument.
	NodeTypeArg NodeType = "arg"
	// NodeTypePipeline represents a pipeline (|).
	NodeTypePipeline NodeType = "pipeline"
	// NodeTypeSequence represents command sequence (; or && or ||).
	NodeTypeSequence NodeType = "sequence"
	// NodeTypeRedirect represents I/O redirection.
	NodeTypeRedirect NodeType = "redirect"
	// NodeTypeSubshell represents a subshell (( )).
	NodeTypeSubshell NodeType = "subshell"
	// NodeTypeCommandSubst represents command substitution ($() or ``).
	NodeTypeCommandSubst NodeType = "command_subst"
	// NodeTypeBackground represents background execution (&).
	NodeTypeBackground NodeType = "background"
	// NodeTypeVariable represents a variable expansion.
	NodeTypeVariable NodeType = "variable"
	// NodeTypeLiteral represents a literal value.
	NodeTypeLiteral NodeType = "literal"
)

// Redirect represents an I/O redirection.
type Redirect struct {
	// Source is the source file descriptor (e.g., 1 for stdout).
	Source int

	// Destination is the target (file or fd).
	Destination string

	// Type is the redirect type (">", ">>", "<", "<<", etc.).
	Type string

	// IsAppend indicates if it's append mode.
	IsAppend bool
}

// ShellASTParser parses shell commands into an AST.
type ShellASTParser struct {
	logger *slog.Logger
}

// NewShellASTParser creates a new Shell AST parser.
func NewShellASTParser(logger *slog.Logger) *ShellASTParser {
	p := &ShellASTParser{
		logger: logger,
	}

	if p.logger == nil {
		p.logger = slog.Default()
	}

	return p
}

// Parse parses a shell command into an AST.
func (p *ShellASTParser) Parse(input string) (*ShellASTNode, error) {
	input = strings.TrimSpace(input)

	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	// Check for obvious command substitution (highest risk)
	if containsCommandSubstitution(input) {
		cmdSubstNode := &ShellASTNode{
			Type:    NodeTypeCommandSubst,
			Value:   input,
			StartPos: 0,
			EndPos:   len(input),
		}
		return cmdSubstNode, nil
	}

	// Check for pipeline
	if strings.Contains(input, " | ") {
		return p.parsePipeline(input)
	}

	// Check for command sequence
	if matchesSequencePattern(input) {
		return p.parseSequence(input)
	}

	// Check for background execution
	if strings.HasSuffix(input, " &") {
		return p.parseBackground(input)
	}

	// Check for redirection
	if hasRedirection(input) {
		return p.parseWithRedirection(input)
	}

	// Simple command
	return p.parseSimpleCommand(input)
}

// containsCommandSubstitution checks for command substitution patterns.
func containsCommandSubstitution(input string) bool {
	patterns := []string{
		"$(", ")", // $()
		"`", "`",  // backticks
		"${", "}", // ${var}
	}

	for i := 0; i < len(patterns); i += 2 {
		start := patterns[i]
		end := patterns[i+1]
		if strings.Contains(input, start) && strings.Contains(input, end) {
			return true
		}
	}
	return false
}

// matchesSequencePattern checks for command sequence patterns.
func matchesSequencePattern(input string) bool {
	patterns := []string{
		" && ", " || ", " ; ", " & ",
	}
	for _, p := range patterns {
		if strings.Contains(input, p) {
			return true
		}
	}
	return false
}

// hasRedirection checks for redirection operators.
func hasRedirection(input string) bool {
	patterns := []string{">>", ">", "<<", "<", "2>", ">&", "<&"}
	for _, p := range patterns {
		if strings.Contains(input, p) {
			return true
		}
	}
	return false
}

// parsePipeline parses a pipeline (commands connected by |).
func (p *ShellASTParser) parsePipeline(input string) (*ShellASTNode, error) {
	parts := strings.Split(input, " | ")

	commands := make([]*ShellASTNode, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		cmd, err := p.parseSimpleCommand(part)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pipeline component: %w", err)
		}
		commands = append(commands, cmd)
	}

	if len(commands) == 0 {
		return nil, fmt.Errorf("empty pipeline")
	}

	return &ShellASTNode{
		Type:     NodeTypePipeline,
		Children: commands,
		StartPos: 0,
		EndPos:   len(input),
	}, nil
}

// parseSequence parses a command sequence.
func (p *ShellASTParser) parseSequence(input string) (*ShellASTNode, error) {
	// Split by sequence operators while preserving the operator
	re := regexp.MustCompile(`(\s+(?:&&|\|\|;|&|;)\s+)`)
	parts := re.Split(input, -1)

	if len(parts) == 1 {
		return p.parseSimpleCommand(input)
	}

	children := make([]*ShellASTNode, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		cmd, err := p.parseSimpleCommand(part)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sequence component: %w", err)
		}
		children = append(children, cmd)
	}

	return &ShellASTNode{
		Type:     NodeTypeSequence,
		Children: children,
		StartPos: 0,
		EndPos:   len(input),
	}, nil
}

// parseBackground parses background execution.
func (p *ShellASTParser) parseBackground(input string) (*ShellASTNode, error) {
	// Remove trailing " &"
	cmdStr := strings.TrimSpace(strings.TrimSuffix(input, "&"))

	cmd, err := p.parseSimpleCommand(cmdStr)
	if err != nil {
		return nil, err
	}

	return &ShellASTNode{
		Type:     NodeTypeBackground,
		Children: []*ShellASTNode{cmd},
		StartPos: 0,
		EndPos:   len(input),
	}, nil
}

// parseWithRedirection parses a command with redirections.
func (p *ShellASTParser) parseWithRedirection(input string) (*ShellASTNode, error) {
	// Find the command part (everything before first redirect)
	re := regexp.MustCompile(`^(\S+)\s*`)
	match := re.FindStringSubmatch(input)

	var cmdStr string
	var redirects []*Redirect

	if match != nil {
		cmdEnd := len(match[0])
		cmdStr = strings.TrimSpace(input[:cmdEnd])

		// Extract redirections
		redirectStr := strings.TrimSpace(input[cmdEnd:])
		if redirectStr != "" {
			redirects = p.parseRedirects(redirectStr)
		}
	} else {
		cmdStr = input
	}

	cmd, err := p.parseSimpleCommand(cmdStr)
	if err != nil {
		return nil, err
	}

	cmd.Redirects = redirects

	return cmd, nil
}

// parseRedirects parses redirection operators.
func (p *ShellASTParser) parseRedirects(redirectStr string) []*Redirect {
	// Simple regex-based redirect parsing
	re := regexp.MustCompile(`(\d*>|>>|<|<<|2>&1|>&2|<&)(\s*\S+)?`)
	matches := re.FindAllStringSubmatch(redirectStr, -1)

	redirects := make([]*Redirect, 0, len(matches))

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		r := &Redirect{
			Type: match[1],
		}

		// Parse source fd
		switch {
		case strings.HasPrefix(match[1], "2"):
			r.Source = 2
		case strings.HasPrefix(match[1], "1"):
			r.Source = 1
		case strings.HasPrefix(match[1], "0"):
			r.Source = 0
		default:
			r.Source = 1 // Default to stdout
		}

		// Check for append mode
		r.IsAppend = match[1] == ">>"

		// Get destination
		if len(match) > 2 && match[2] != "" {
			r.Destination = strings.TrimSpace(match[2])
		}

		redirects = append(redirects, r)
	}

	return redirects
}

// parseSimpleCommand parses a simple command (command + args).
func (p *ShellASTParser) parseSimpleCommand(input string) (*ShellASTNode, error) {
	// Tokenize while respecting quotes
	tokens := tokenize(input)

	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	cmd := &ShellASTNode{
		Type:     NodeTypeCommand,
		Name:     tokens[0],
		Args:     make([]*ShellASTNode, 0),
		StartPos: 0,
		EndPos:   len(input),
	}

	// Parse arguments
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		argNode := &ShellASTNode{
			Type:  NodeTypeArg,
			Value: token,
		}

		// Check if it's a variable
		if strings.HasPrefix(token, "$") {
			argNode.Type = NodeTypeVariable
		}

		cmd.Args = append(cmd.Args, argNode)
	}

	return cmd, nil
}

// tokenize splits a shell command into tokens, respecting quotes.
func tokenize(input string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range input {
		if !inQuote && (r == '"' || r == '\'') {
			inQuote = true
			quoteChar = r
			continue
		}

		if inQuote && r == quoteChar {
			inQuote = false
			quoteChar = 0
			continue
		}

		if !inQuote && (r == ' ' || r == '\t') {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// ========================================
// Semantic Command Analyzer
// ========================================

// CommandCategory represents the category of a command.
type CommandCategory string

const (
	// CategoryFileOp represents file operations.
	CategoryFileOp CommandCategory = "file_operation"
	// CategoryNetOp represents network operations.
	CategoryNetOp CommandCategory = "network_operation"
	// CategorySysOp represents system operations.
	CategorySysOp CommandCategory = "system_operation"
	// CategoryDataOp represents data operations.
	CategoryDataOp CommandCategory = "data_operation"
	// CategoryProcess represents process operations.
	CategoryProcess CommandCategory = "process_operation"
	// CategoryAuth represents authentication/authorization.
	CategoryAuth CommandCategory = "authentication"
	// CategoryContainer represents container operations.
	CategoryContainer CommandCategory = "container_operation"
	// CategoryKernel represents kernel operations.
	CategoryKernel CommandCategory = "kernel_operation"
	// CategoryBenign represents benign operations.
	CategoryBenign CommandCategory = "benign"
)

// SemanticAnalysisResult contains the result of semantic command analysis.
type SemanticAnalysisResult struct {
	// Category is the command category.
	Category CommandCategory

	// RiskLevel is the risk level (0-10).
	RiskLevel int

	// RiskFactors are specific risk factors identified.
	RiskFactors []string

	// IsSafe indicates if the command is considered safe.
	IsSafe bool

	// Details contains additional analysis details.
	Details map[string]interface{}
}

// SemanticAnalyzer performs semantic analysis on shell commands.
type SemanticAnalyzer struct {
	parser  *ShellASTParser
	logger  *slog.Logger
	mu      sync.RWMutex

	// whitelist contains allowed commands.
	whitelist map[string]bool

	// blacklist contains blocked commands.
	blacklist map[string]bool

	// categoryMap maps commands to categories.
	categoryMap map[string]CommandCategory
}

// NewSemanticAnalyzer creates a new semantic analyzer.
func NewSemanticAnalyzer(logger *slog.Logger) *SemanticAnalyzer {
	sa := &SemanticAnalyzer{
		parser:      NewShellASTParser(logger),
		logger:      logger,
		whitelist:   make(map[string]bool),
		blacklist:   make(map[string]bool),
		categoryMap: make(map[string]CommandCategory),
	}

	if sa.logger == nil {
		sa.logger = slog.Default()
	}

	// Initialize default whitelists
	sa.initWhitelist()
	sa.initBlacklist()
	sa.initCategoryMap()

	return sa
}

// initWhitelist initializes the default whitelist.
func (sa *SemanticAnalyzer) initWhitelist() {
	safeCommands := []string{
		"ls", "cd", "pwd", "echo", "cat", "head", "tail", "less", "more",
		"grep", "find", "awk", "sed", "sort", "uniq", "wc", "cut", "tr",
		"mkdir", "touch", "cp", "mv", "rmdir",
		"git", "go", "npm", "yarn", "pip", "python", "node",
		"docker", "docker-compose", "kubectl",
		"date", "time", "which", "whoami", "id", "hostname", "uname",
		"env", "printenv", "export",
	}

	for _, cmd := range safeCommands {
		sa.whitelist[cmd] = true
	}
}

// initBlacklist initializes the default blacklist.
func (sa *SemanticAnalyzer) initBlacklist() {
	dangerousCommands := []string{
		"rm -rf /", "mkfs", "dd if=/dev/zero", "wipefs",
		":(){:|:&};:", "fork", // fork bomb
		"chmod 777", "chown -R root:root",
		"nc -e", "ncat -e", // reverse shell
		"wget | sh", "curl | sh", // download and execute
		"insmod", "rmmod", "modprobe",
		"pkexec", "su ", "sudo su",
	}

	for _, cmd := range dangerousCommands {
		sa.blacklist[cmd] = true
	}
}

// initCategoryMap initializes the command category map.
func (sa *SemanticAnalyzer) initCategoryMap() {
	categories := map[string]CommandCategory{
		// File operations
		"rm":    CategoryFileOp,
		"rmdir": CategoryFileOp,
		"cp":    CategoryFileOp,
		"mv":    CategoryFileOp,
		"mkdir": CategoryFileOp,
		"touch": CategoryFileOp,
		"chmod": CategoryFileOp,
		"chown": CategoryFileOp,
		"chgrp": CategoryFileOp,

		// Network operations
		"curl":    CategoryNetOp,
		"wget":    CategoryNetOp,
		"nc":      CategoryNetOp,
		"ncat":    CategoryNetOp,
		"ssh":     CategoryNetOp,
		"scp":     CategoryNetOp,
		"rsync":   CategoryNetOp,
		"netstat": CategoryNetOp,
		"ss":      CategoryNetOp,

		// System operations
		"systemctl":  CategorySysOp,
		"service":   CategorySysOp,
		"init":      CategorySysOp,
		"shutdown":  CategorySysOp,
		"reboot":    CategorySysOp,
		"halt":      CategorySysOp,
		"telinit":   CategorySysOp,

		// Process operations
		"kill":    CategoryProcess,
		"killall": CategoryProcess,
		"pkill":   CategoryProcess,
		"top":     CategoryProcess,
		"ps":      CategoryProcess,

		// Data operations
		"psql":    CategoryDataOp,
		"mysql":   CategoryDataOp,
		"mongosh": CategoryDataOp,
		"redis-cli": CategoryDataOp,

		// Container operations
		"docker":    CategoryContainer,
		"podman":   CategoryContainer,
		"kubectl":   CategoryContainer,
		"crictl":   CategoryContainer,

		// Kernel operations
		"insmod":  CategoryKernel,
		"rmmod":   CategoryKernel,
		"modprobe": CategoryKernel,
		"lsmod":   CategoryKernel,

		// Auth operations
		"sudo":    CategoryAuth,
		"su":      CategoryAuth,
		"doas":    CategoryAuth,
		"passwd":  CategoryAuth,
		"useradd": CategoryAuth,
		"userdel": CategoryAuth,
	}

	for cmd, cat := range categories {
		sa.categoryMap[cmd] = cat
	}
}

// Analyze performs semantic analysis on a command string.
func (sa *SemanticAnalyzer) Analyze(input string) (*SemanticAnalysisResult, error) {
	// Parse into AST
	ast, err := sa.parser.Parse(input)
	if err != nil {
		// If parsing fails, do heuristic analysis
		return sa.heuristicAnalysis(input)
	}

	return sa.analyzeAST(ast, input)
}

// analyzeAST performs semantic analysis on an AST.
func (sa *SemanticAnalyzer) analyzeAST(ast *ShellASTNode, originalInput string) (*SemanticAnalysisResult, error) {
	result := &SemanticAnalysisResult{
		Details: make(map[string]interface{}),
	}

	// Check for dangerous node types
	if sa.checkDangerousNodeTypes(ast) {
		result.RiskLevel = 10
		result.RiskFactors = append(result.RiskFactors, "Command substitution detected (high risk)")
		result.Category = CategorySysOp
		result.IsSafe = false
		return result, nil
	}

	// Analyze the main command
	if ast.Type == NodeTypeCommand {
		cmdName := strings.ToLower(ast.Name)
		result.Category = sa.categoryMap[cmdName]

		// Check whitelist
		if sa.whitelist[cmdName] {
			result.IsSafe = true
			result.RiskLevel = 0
			result.Category = CategoryBenign
			return result, nil
		}

		// Check blacklist
		if sa.blacklist[cmdName] {
			result.IsSafe = false
			result.RiskLevel = 10
			result.RiskFactors = append(result.RiskFactors, "Blacklisted command")
			return result, nil
		}

		// Calculate risk based on arguments
		riskLevel, factors := sa.analyzeArgs(ast.Args)
		result.RiskLevel = riskLevel
		result.RiskFactors = append(result.RiskFactors, factors...)

		// Determine safety
		result.IsSafe = riskLevel < 5
	}

	// Analyze children (pipeline, sequence, etc.)
	for _, child := range ast.Children {
		childResult, _ := sa.analyzeAST(child, originalInput)
		if childResult.RiskLevel > result.RiskLevel {
			result.RiskLevel = childResult.RiskLevel
		}
		result.RiskFactors = append(result.RiskFactors, childResult.RiskFactors...)
	}

	// Check for dangerous argument patterns
	dangerArgsRisk, dangerArgsFactors := sa.checkDangerousArguments(originalInput)
	result.RiskLevel = max(result.RiskLevel, dangerArgsRisk)
	result.RiskFactors = append(result.RiskFactors, dangerArgsFactors...)

	if result.RiskLevel >= 7 {
		result.IsSafe = false
	}

	return result, nil
}

// checkDangerousNodeTypes checks for dangerous AST node types.
func (sa *SemanticAnalyzer) checkDangerousNodeTypes(ast *ShellASTNode) bool {
	dangerousTypes := map[NodeType]bool{
		NodeTypeCommandSubst: true,
		NodeTypeSubshell:     true,
	}

	if dangerousTypes[ast.Type] {
		return true
	}

	for _, child := range ast.Children {
		if sa.checkDangerousNodeTypes(child) {
			return true
		}
	}

	for _, arg := range ast.Args {
		if sa.checkDangerousNodeTypes(arg) {
			return true
		}
	}

	return false
}

// analyzeArgs analyzes command arguments for risk factors.
func (sa *SemanticAnalyzer) analyzeArgs(args []*ShellASTNode) (int, []string) {
	riskLevel := 0
	var factors []string

	dangerousPatterns := map[string]int{
		"/etc/passwd":    7,
		"/etc/shadow":    10,
		"~/.ssh/":        8,
		"/proc/":         5,
		"/dev/":          6,
		"sudo":           6,
		"su ":            8,
		"-rf":            5,
		"-rf /":          10,
		"-rf *":          7,
		"--privileged":   8,
		"--network=host": 7,
		"-v /":           8,
	}

	argValues := make([]string, len(args))
	for i, arg := range args {
		argValues[i] = arg.Value
	}

	argsStr := strings.Join(argValues, " ")

	for pattern, risk := range dangerousPatterns {
		if strings.Contains(argsStr, pattern) {
			riskLevel = max(riskLevel, risk)
			factors = append(factors, fmt.Sprintf("Dangerous argument pattern: %s", pattern))
		}
	}

	return riskLevel, factors
}

// checkDangerousArguments checks for dangerous argument patterns.
func (sa *SemanticAnalyzer) checkDangerousArguments(input string) (int, []string) {
	riskLevel := 0
	var factors []string

	dangerousPatterns := []struct {
		pattern string
		risk    int
		desc    string
	}{
		{"rm -rf /", 10, "Recursive delete from root"},
		{"rm -rf *", 8, "Recursive delete all files"},
		{"dd if=", 9, "Direct device access"},
		{"mkfs", 10, "Filesystem format"},
		{"chmod 777", 7, "Insecure permissions"},
		{"chown -R", 6, "Recursive ownership change"},
		{"> /dev/", 7, "Direct device write"},
		{"curl | sh", 8, "Download and execute"},
		{"wget | sh", 8, "Download and execute"},
		{"nc -e", 10, "Netcat reverse shell"},
		{"bash -i", 8, "Interactive bash"},
		{"python -c", 7, "Python code execution"},
		{"perl -e", 7, "Perl code execution"},
	}

	lowerInput := strings.ToLower(input)
	for _, p := range dangerousPatterns {
		if strings.Contains(lowerInput, strings.ToLower(p.pattern)) {
			riskLevel = max(riskLevel, p.risk)
			factors = append(factors, p.desc)
		}
	}

	return riskLevel, factors
}

// heuristicAnalysis performs heuristic analysis when AST parsing fails.
func (sa *SemanticAnalyzer) heuristicAnalysis(input string) (*SemanticAnalysisResult, error) {
	result := &SemanticAnalysisResult{
		Details:       make(map[string]interface{}),
		Category:      CategoryBenign,
		RiskLevel:     0,
		RiskFactors:   []string{"Heuristic analysis (parsing failed)"},
		IsSafe:        true,
	}

	lowerInput := strings.ToLower(input)

	// Check for known dangerous patterns
	dangerIndicators := []struct {
		indicators []string
		risk       int
		category   CommandCategory
	}{
		{[]string{"rm -rf", "del /"}, 10, CategoryFileOp},
		{[]string{"mkfs", "dd if=", "wipefs"}, 10, CategorySysOp},
		{[]string{"nc -e", "ncat -e", "bash -i"}, 10, CategoryNetOp},
		{[]string{"sudo su", "pkexec", "su -"}, 9, CategoryAuth},
		{[]string{"insmod", "rmmod", "modprobe"}, 9, CategoryKernel},
		{[]string{"curl |", "wget |"}, 7, CategoryNetOp},
		{[]string{"chmod 777"}, 6, CategoryFileOp},
	}

	for _, indicator := range dangerIndicators {
		for _, ind := range indicator.indicators {
			if strings.Contains(lowerInput, ind) {
				result.RiskLevel = max(result.RiskLevel, indicator.risk)
				result.Category = indicator.category
				result.RiskFactors = append(result.RiskFactors,
					fmt.Sprintf("Dangerous pattern: %s", ind))
				result.IsSafe = false
			}
		}
	}

	// Check for known safe commands
	safeCommands := []string{"ls", "pwd", "cd", "cat", "head", "tail", "grep", "find"}
	for _, cmd := range safeCommands {
		cmdPattern := "^" + cmd + "\\b"
		if matched, _ := regexp.MatchString(cmdPattern, lowerInput); matched {
			if result.RiskLevel < 3 {
				result.RiskLevel = 0
				result.IsSafe = true
				result.Category = CategoryBenign
			}
			break
		}
	}

	return result, nil
}

// AddToWhitelist adds a command to the whitelist.
func (sa *SemanticAnalyzer) AddToWhitelist(command string) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.whitelist[command] = true
}

// AddToBlacklist adds a command to the blacklist.
func (sa *SemanticAnalyzer) AddToBlacklist(command string) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.blacklist[command] = true
}

// SetCategory sets the category for a command.
func (sa *SemanticAnalyzer) SetCategory(command string, category CommandCategory) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.categoryMap[command] = category
}

// ========================================
// HTML/Script Sanitizer
// ========================================

// HTMLSanitizer sanitizes HTML content to prevent XSS.
type HTMLSanitizer struct {
	logger *slog.Logger
}

// NewHTMLSanitizer creates a new HTML sanitizer.
func NewHTMLSanitizer(logger *slog.Logger) *HTMLSanitizer {
	s := &HTMLSanitizer{
		logger: logger,
	}

	if s.logger == nil {
		s.logger = slog.Default()
	}

	return s
}

// Sanitize removes potentially dangerous HTML/Script content.
func (s *HTMLSanitizer) Sanitize(input string) string {
	// Use golang.org/x/net/html to parse and sanitize
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		// Fallback: basic sanitization
		return s.basicSanitize(input)
	}

	return s.sanitizeNode(doc)
}

// sanitizeNode recursively sanitizes an HTML node.
func (s *HTMLSanitizer) sanitizeNode(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}

	if n.Type == html.ElementNode {
		// Check if tag is allowed
		tagName := strings.ToLower(n.Data)
		allowedTags := map[string]bool{
			"p": true, "br": true, "b": true, "i": true, "u": true,
			"strong": true, "em": true, "code": true, "pre": true,
			"ul": true, "ol": true, "li": true, "a": true, "h1": true,
			"h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
			"span": true, "div": true,
		}

		if !allowedTags[tagName] {
			return ""
		}

		// For links, sanitize href
		if tagName == "a" {
			for _, attr := range n.Attr {
				if strings.ToLower(attr.Key) == "href" {
					if !strings.HasPrefix(attr.Val, "http://") &&
						!strings.HasPrefix(attr.Val, "https://") &&
						!strings.HasPrefix(attr.Val, "mailto:") {
						// Remove unsafe hrefs
						return ""
					}
				}
			}
		}
	}

	// Recursively sanitize children
	var result strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		result.WriteString(s.sanitizeNode(c))
	}

	return result.String()
}

// basicSanitize provides basic HTML sanitization as fallback.
func (s *HTMLSanitizer) basicSanitize(input string) string {
	// Remove script tags and content
	scriptRe := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	input = scriptRe.ReplaceAllString(input, "")

	// Remove style tags and content
	styleRe := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	input = styleRe.ReplaceAllString(input, "")

	// Remove on* event handlers
	_ = regexp.MustCompile(`(?i)\bon\w+\s*=`)

	// Note: This is a basic fallback, the full parser is more reliable
	return input
}
