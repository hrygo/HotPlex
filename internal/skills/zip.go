package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 容量/安全阈值（spec §3.3 A）。zip body 的 20MB 上限在 handler 层由
// http.MaxBytesReader 兜底；此处的阈值约束解压阶段。
const (
	maxTotalUncompressed = 50 << 20 // 解压总上限 50MB
	maxSingleFile        = 5 << 20  // 单文件上限 5MB
	maxEntries           = 500      // entry 数上限
	maxCompressionRatio  = 100      // 压缩率 >100× 拒
	maxDescriptionRunes  = 1024     // description 独立严格校验（spec §3.3 B4）
	maxSkillNameLen      = 64
)

// 校验语义错误。handler 层据此映射 HTTP 错误码（spec §5）：
//   - ErrInvalidZip      → 400 SKILL_INVALID_ZIP
//   - ErrInvalidFormat   → 400 SKILL_INVALID_FORMAT
//   - ErrFileTypeBlocked → 400 SKILL_FILE_TYPE_BLOCKED
var (
	ErrInvalidZip      = errors.New("skill: invalid, corrupt, or oversized zip")
	ErrInvalidFormat   = errors.New("skill: invalid skill format")
	ErrFileTypeBlocked = errors.New("skill: blocked file type")
)

// skillNameRegexp 校验 skill name：小写字母/数字，连字符分段，1-64 字符
// （spec §3.3 B3）。正则本身排除路径分隔符与 ".."，是 zip-slip 的第一道防线。
var skillNameRegexp = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// allowedFileExts 是包内允许的文件扩展名白名单（spec §3.3 B7）：拒可执行/二进制。
var allowedFileExts = map[string]bool{
	".md": true, ".markdown": true,
	".json": true, ".yaml": true, ".yml": true, ".txt": true, ".toml": true,
	".py": true, ".sh": true,
	".png": true, ".jpg": true, ".jpeg": true, ".svg": true,
}

// extractedSkill 是 zip 解压 + 安全校验的产物。
type extractedSkill struct {
	name        string
	description string
	body        string   // SKILL.md 全文
	files       []string // 相对 skill 根的文件路径（含 SKILL.md）
	rootRel     string   // skill 内容根相对 staging 的路径；""=扁平，"<name>"=单顶层
	skillMDName string   // SKILL.md 文件名（"SKILL.md" 或 "skill.md"）
}

// safeExtractJoin 安全拼接 staging 目录与 zip entry 的相对路径，防 zip-slip
// （路径穿越）。
//
// spec §3.3 A 原写"复用 security.SafePathJoin"，但 SafePathJoin 对 joined 路径
// 调 filepath.EvalSymlinks —— 解压目标文件此时尚不存在，EvalSymlinks 会直接
// 失败使全部正常解压都报错。此处采用与 SafePathJoin 等价的防护（Clean + 拒绝
// 绝对路径/".." + Join + 前缀校验），仅省去 EvalSymlinks：staging 为本包新建的
// 受控临时目录，entry 相对路径已严格 Clean，字符串前缀校验足以保证 dest 落在
// staging 内。调用方另做一次显式 HasPrefix 构成双保险。
func safeExtractJoin(staging, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	clean := filepath.Clean(rel)
	sep := string(filepath.Separator)
	if clean == ".." || strings.HasPrefix(clean, ".."+sep) {
		return "", fmt.Errorf("traversal attempt: %s", rel)
	}
	joined := filepath.Join(staging, clean)
	if joined != staging && !strings.HasPrefix(joined, staging+sep) {
		return "", fmt.Errorf("path escapes staging: %s", rel)
	}
	return joined, nil
}

// extractZip 将 zr 解压到 tempStaging（必须已存在且与最终落盘目标同文件系统），
// 并完成全部安全层（zip-slip/炸弹/恶意 entry/类型白名单）与格式层（结构、
// frontmatter、name 正则、name==父目录名、description 长度）校验。
//
// 失败时调用方负责清理 tempStaging。
func extractZip(zr *zip.Reader, tempStaging string) (*extractedSkill, error) {
	if len(zr.File) == 0 {
		return nil, fmt.Errorf("%w: empty archive", ErrInvalidZip)
	}
	if len(zr.File) > maxEntries {
		return nil, fmt.Errorf("%w: too many entries (%d > %d)", ErrInvalidZip, len(zr.File), maxEntries)
	}

	stagingBase := filepath.Clean(tempStaging)
	sep := string(filepath.Separator)
	var totalUncompressed uint64

	for _, f := range zr.File {
		// 拒嵌套 zip（spec §3.3 A 禁嵌套 zip）。
		if strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			return nil, fmt.Errorf("%w: nested zip not allowed", ErrInvalidZip)
		}
		// 目录 entry（显式或以 / 结尾）跳过——目录由文件路径隐式创建。
		if f.Mode().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		// 仅接受常规文件 entry：拒 symlink/device/pipe（spec §3.3 A 恶意 entry）。
		if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: non-regular entry %q", ErrInvalidZip, f.Name)
		}
		// 文件类型白名单（spec §3.3 B7）。
		if !allowedFileExts[strings.ToLower(filepath.Ext(f.Name))] {
			return nil, fmt.Errorf("%w: %q", ErrFileTypeBlocked, f.Name)
		}
		// 单文件大小（spec §3.3 A 单文件 ≤5MB）。
		if f.UncompressedSize64 > maxSingleFile {
			return nil, fmt.Errorf("%w: file too large %q (%d bytes)", ErrInvalidZip, f.Name, f.UncompressedSize64)
		}
		// 压缩率（spec §3.3 A 压缩率 >100× 拒）。
		if f.CompressedSize64 > 0 && f.UncompressedSize64 > uint64(maxCompressionRatio)*f.CompressedSize64 {
			return nil, fmt.Errorf("%w: suspicious compression ratio %q", ErrInvalidZip, f.Name)
		}
		// 解压总大小（spec §3.3 A 解压总 ≤50MB）。
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxTotalUncompressed {
			return nil, fmt.Errorf("%w: total uncompressed exceeds %d bytes", ErrInvalidZip, maxTotalUncompressed)
		}

		// zip-slip 双保险：safeExtractJoin（Clean/拒绝对路径与".."→Join→前缀校验）
		// + 显式 HasPrefix 再查一次（spec §3.3 A）。
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		dest, err := safeExtractJoin(stagingBase, rel)
		if err != nil {
			return nil, fmt.Errorf("%w: unsafe path %q: %w", ErrInvalidZip, f.Name, err)
		}
		if dest != stagingBase && !strings.HasPrefix(dest, stagingBase+sep) {
			return nil, fmt.Errorf("%w: path escapes staging %q", ErrInvalidZip, f.Name)
		}
		if err := extractFile(f, dest); err != nil {
			return nil, fmt.Errorf("%w: extract %q: %w", ErrInvalidZip, f.Name, err)
		}
	}

	return locateAndValidateSkill(stagingBase)
}

// extractFile 解压单个常规 zip entry 到 dest。io.LimitReader 兜底防 zip 头
// 撒谎（声称小实则流大），复制超限即视为损坏。
func extractFile(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	n, err := io.Copy(out, io.LimitReader(rc, maxSingleFile+1))
	if err != nil {
		return err
	}
	if n > int64(maxSingleFile) {
		return fmt.Errorf("actual size exceeds %d bytes", maxSingleFile)
	}
	return nil
}

// ZipReaderFromFile 从 multipart 上传文件构造 zip.Reader，优先利用 *os.File 的
// ReaderAt（multipart >32KiB 溢出磁盘时）避免全量载内存（20MB×N 并发 DoS，spec
// review P2#5）；小文件（内存 *sectionReader）回退 io.ReadAll。f 由调用方 Close。
func ZipReaderFromFile(f io.ReadCloser) (*zip.Reader, error) {
	if osFile, ok := f.(*os.File); ok {
		if info, err := osFile.Stat(); err == nil {
			return zip.NewReader(osFile, info.Size())
		}
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
}

// locateAndValidateSkill 在 staging 内定位 SKILL.md（扁平 staging/SKILL.md
// 或单顶层 staging/<dir>/SKILL.md）并完成 frontmatter/name/description 校验。
func locateAndValidateSkill(staging string) (*extractedSkill, error) {
	// 优先扁平：staging/SKILL.md。
	if md, ok := findSkillMD(staging); ok {
		return parseAndValidate(staging, md, "")
	}
	// 单顶层目录：恰好一个一级子目录，内含 SKILL.md。
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil, fmt.Errorf("%w: read staging: %w", ErrInvalidFormat, err)
	}
	var topDir string
	for _, e := range entries {
		full := filepath.Join(staging, e.Name())
		if isSymlink(full) {
			continue
		}
		if e.IsDir() {
			if topDir != "" {
				return nil, fmt.Errorf("%w: multiple top-level directories", ErrInvalidFormat)
			}
			topDir = e.Name()
		}
	}
	if topDir == "" {
		return nil, fmt.Errorf("%w: no SKILL.md found", ErrInvalidFormat)
	}
	root := filepath.Join(staging, topDir)
	md, ok := findSkillMD(root)
	if !ok {
		return nil, fmt.Errorf("%w: no SKILL.md in %q", ErrInvalidFormat, topDir)
	}
	return parseAndValidate(root, md, topDir)
}

// findSkillMD 在 dir 下查找 SKILL.md（优先）或 skill.md。返回文件绝对路径。
func findSkillMD(dir string) (string, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

// parseAndValidate 解析 SKILL.md frontmatter 并完成格式校验。skillRoot 是
// staging 内 skill 内容根；dirName 是其目录名（扁平时为 ""，用于 name==父目录名校验）。
func parseAndValidate(skillRoot, skillMDPath, dirName string) (*extractedSkill, error) {
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return nil, fmt.Errorf("%w: read SKILL.md: %w", ErrInvalidFormat, err)
	}
	fm := extractFrontmatter(data)
	if fm == nil {
		return nil, fmt.Errorf("%w: missing frontmatter", ErrInvalidFormat)
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: frontmatter missing name", ErrInvalidFormat)
	}
	if len(name) > maxSkillNameLen || !skillNameRegexp.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name %q", ErrInvalidFormat, name)
	}
	// name 必须等于父目录名（单顶层模式）；扁平模式 dirName=="" 跳过（spec §3.3 B3）。
	if dirName != "" && dirName != name {
		return nil, fmt.Errorf("%w: name %q must equal parent dir %q", ErrInvalidFormat, name, dirName)
	}
	desc := CollapseSpaces(strings.ReplaceAll(strings.TrimSpace(fm.Description), "\n", " "))
	descRunes := len([]rune(desc))
	if descRunes == 0 || descRunes > maxDescriptionRunes {
		return nil, fmt.Errorf("%w: description length %d out of range [1,%d]", ErrInvalidFormat, descRunes, maxDescriptionRunes)
	}
	files, err := collectFiles(skillRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: collect files: %w", ErrInvalidFormat, err)
	}
	return &extractedSkill{
		name:        name,
		description: desc,
		body:        string(data),
		files:       files,
		rootRel:     dirName, // 扁平为 ""，单顶层为 ==name 的目录名
		skillMDName: filepath.Base(skillMDPath),
	}, nil
}

// collectFiles 收集 root 下所有常规文件的相对路径（含 SKILL.md，跳过 symlink）。
func collectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || isSymlink(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
