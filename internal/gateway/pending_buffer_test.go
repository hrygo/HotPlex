package gateway

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func newInputEnvelope(t *testing.T, sid, content string) *events.Envelope {
	t.Helper()
	return events.NewEnvelope("evt_orig", sid, 0, events.Input, map[string]any{
		"content":           content,
		"client_message_id": "cmid_orig",
	})
}

func TestPendingBuffer_DrainAndMerge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inputs  []string
		wantSub []string // merged 中应包含的子串
		single  bool
	}{
		{"single passthrough", []string{"继续"}, []string{"继续"}, true},
		{"multi merged numbered", []string{"继续", "补充", "换角度"},
			[]string{"3 条消息", "1. 继续", "2. 补充", "3. 换角度"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pb := NewPendingBuffer()
			env := newInputEnvelope(t, "sess_1", "orig")
			for _, c := range tc.inputs {
				pb.Append("sess_1", c, env)
			}
			merged, repr, ok := pb.DrainAndMerge("sess_1")
			require.True(t, ok)
			require.NotNil(t, repr)
			if tc.single {
				require.Equal(t, tc.inputs[0], merged)
			} else {
				for _, s := range tc.wantSub {
					require.Contains(t, merged, s)
				}
			}
			// drain 后清空
			_, _, ok2 := pb.DrainAndMerge("sess_1")
			require.False(t, ok2)
		})
	}
}

func TestPendingBuffer_DedupAdjacentAndCap(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	// 相邻完全相同去重
	pb.Append("s", "同", env)
	pb.Append("s", "同", env)
	merged, _, _ := pb.DrainAndMerge("s")
	require.Equal(t, "同", merged) // 只剩一条

	// 上限 20：塞 25 条，只保留最新 20，合并后编号 1..20
	for i := 0; i < 25; i++ {
		pb.Append("s", fmt.Sprintf("m%d", i), env)
	}
	merged, _, _ = pb.DrainAndMerge("s")
	require.Contains(t, merged, "20 条消息")
	require.Contains(t, merged, "1. m5") // 最旧保留的是 m5（丢 m0..m4）
	require.Contains(t, merged, "20. m24")
}

func TestPendingBuffer_Concurrent(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() { pb.Append("s", "c", env); done <- struct{}{} }()
		go func() { pb.DrainAndMerge("s"); done <- struct{}{} }()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestCloneForReplay_NewIDs(t *testing.T) {
	t.Parallel()
	orig := newInputEnvelope(t, "s", "old")
	orig.Event.Data.(map[string]any)["client_message_id"] = "cmid_orig"
	c := cloneForReplay(orig, "new content")
	require.NotEqual(t, orig.ID, c.ID)
	require.NotEqual(t, "cmid_orig", c.Event.Data.(map[string]any)["client_message_id"])
	require.Equal(t, "new content", c.Event.Data.(map[string]any)["content"])
	require.Equal(t, "s", c.SessionID) // 复用 session
	require.Zero(t, c.Seq)             // 由 hub 重分配
}
