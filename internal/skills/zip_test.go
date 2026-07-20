package skills

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeZip 从 name→content 映射构造一个内存 zip.Reader（测试 helper，同包复用）。
func makeZip(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	return zr
}

// makeZipSymlink 构造一个含 symlink entry 的 zip（测恶意 entry 过滤）。
func makeZipSymlink(t *testing.T) *zip.Reader {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	h := &zip.FileHeader{Name: "evil-link", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0o777)
	fw, err := w.CreateHeader(h)
	require.NoError(t, err)
	_, err = fw.Write([]byte("/etc/passwd"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	return zr
}

const validFM = "---\nname: my-skill\ndescription: a useful skill\n---\n# My Skill\nbody\n"

func runExtract(t *testing.T, zr *zip.Reader) (*extractedSkill, error) {
	t.Helper()
	return extractZip(zr, t.TempDir())
}

// ─── 合法结构 ────────────────────────────────────────────────────────────────

func TestExtractZip_FlatSKILLMD(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{"SKILL.md": validFM})
	es, err := runExtract(t, zr)
	require.NoError(t, err)
	require.Equal(t, "my-skill", es.name)
	require.Equal(t, "a useful skill", es.description)
	require.Equal(t, "", es.rootRel, "扁平结构 rootRel 为空")
	require.Equal(t, "SKILL.md", es.skillMDName)
	require.Contains(t, es.files, "SKILL.md")
	require.Contains(t, es.body, "# My Skill")
}

func TestExtractZip_SingleTopDir(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"my-skill/SKILL.md":     validFM,
		"my-skill/reference.md": "# ref\n",
	})
	es, err := runExtract(t, zr)
	require.NoError(t, err)
	require.Equal(t, "my-skill", es.name)
	require.Equal(t, "my-skill", es.rootRel, "单顶层 rootRel 必须等于目录名")
	require.Len(t, es.files, 2)
}

func TestExtractZip_MultipleTopDirs(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"my-skill/SKILL.md": validFM,
		"other/x.md":        "# x\n",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

func TestExtractZip_NoSKILLMD(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{"my-skill/notes.md": "# notes\n"})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

// ─── name / description 校验（spec §3.3 B3/B4）──────────────────────────────

func TestExtractZip_NameNotEqualDir(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"other-dir/SKILL.md": "---\nname: my-skill\ndescription: d\n---\n",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

func TestExtractZip_InvalidNameRegex(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"UpperCase":     "---\nname: MySkill\ndescription: d\n---\n",
		"underscore":    "---\nname: my_skill\ndescription: d\n---\n",
		"trailing-dash": "---\nname: my-\ndescription: d\n---\n",
		"slash":         "---\nname: my/skill\ndescription: d\n---\n",
		"dot":           "---\nname: my.skill\ndescription: d\n---\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			zr := makeZip(t, map[string]string{"SKILL.md": body})
			_, err := runExtract(t, zr)
			require.ErrorIs(t, err, ErrInvalidFormat)
		})
	}
}

func TestExtractZip_DescriptionTooLong(t *testing.T) {
	t.Parallel()
	long := make([]rune, maxDescriptionRunes+1)
	for i := range long {
		long[i] = 'x'
	}
	zr := makeZip(t, map[string]string{"SKILL.md": "---\nname: foo\ndescription: " + string(long) + "\n---\n"})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

func TestExtractZip_EmptyDescription(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{"SKILL.md": "---\nname: foo\ndescription:\n---\n"})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

func TestExtractZip_MissingFrontmatter(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{"SKILL.md": "# no frontmatter here\n"})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidFormat)
}

// ─── 安全层（spec §3.3 A）──────────────────────────────────────────────────

func TestExtractZip_EmptyArchive(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}

func TestExtractZip_ZipSlip(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"SKILL.md":   validFM,
		"../evil.md": "# evil\n",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}

func TestExtractZip_AbsolutePath(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"SKILL.md":     validFM,
		"/etc/evil.md": "# evil\n",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}

func TestExtractZip_FileTypeBlocked(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"SKILL.md": validFM,
		"run.exe":  "MZbinary",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrFileTypeBlocked)
}

func TestExtractZip_NestedZip(t *testing.T) {
	t.Parallel()
	zr := makeZip(t, map[string]string{
		"SKILL.md":   validFM,
		"bundle.zip": "PK...",
	})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}

func TestExtractZip_SymlinkEntry(t *testing.T) {
	t.Parallel()
	zr := makeZipSymlink(t)
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}

func TestExtractZip_HighCompressionRatio(t *testing.T) {
	t.Parallel()
	// 200KB 全同字节 → deflate 压缩率极高（>>100×），命中炸弹防护（spec §3.3 A）。
	payload := bytes.Repeat([]byte{'A'}, 200<<10)
	zr := makeZip(t, map[string]string{"SKILL.md": validFM, "blob.json": string(payload)})
	_, err := runExtract(t, zr)
	require.ErrorIs(t, err, ErrInvalidZip)
}
