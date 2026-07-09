package feishu

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestCardHeaderToMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		h    cardHeader
		want map[string]any
	}{
		{
			name: "title only",
			h:    cardHeader{Title: "Bot"},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
			},
		},
		{
			name: "title and subtitle",
			h:    cardHeader{Title: "Bot", Subtitle: "生成中..."},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"subtitle": map[string]any{"tag": "plain_text", "content": "生成中..."},
			},
		},
		{
			name: "title and template",
			h:    cardHeader{Title: "Bot", Template: "blue"},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"template": "blue",
			},
		},
		{
			name: "title with tags",
			h: cardHeader{Title: "Bot", Tags: []cardTag{
				{Text: "pending", Color: "orange"},
			}},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
				"text_tag_list": []map[string]any{
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "pending"}, "color": "orange"},
				},
			},
		},
		{
			name: "all fields",
			h: cardHeader{
				Title: "Bot", Subtitle: "sub", Template: "wathet",
				Tags: []cardTag{{Text: "v1", Color: "blue"}, {Text: "v2"}},
			},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"subtitle": map[string]any{"tag": "plain_text", "content": "sub"},
				"template": "wathet",
				"text_tag_list": []map[string]any{
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "v1"}, "color": "blue"},
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "v2"}},
				},
			},
		},
		{
			name: "empty title returns nil",
			h:    cardHeader{Template: "blue"},
			want: nil,
		},
		{
			name: "tag with empty text skipped",
			h: cardHeader{Title: "Bot", Tags: []cardTag{
				{Text: "", Color: "red"},
				{Text: "ok", Color: "green"},
			}},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
				"text_tag_list": []map[string]any{
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "ok"}, "color": "green"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.h.toMap()
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.want["title"], got["title"])
			if s, ok := tt.want["subtitle"]; ok {
				require.Equal(t, s, got["subtitle"])
			}
			if s, ok := tt.want["template"]; ok {
				require.Equal(t, s, got["template"])
			}
			if s, ok := tt.want["text_tag_list"]; ok {
				require.Equal(t, s, got["text_tag_list"])
			}
		})
	}
}

func TestQuestionFooterHint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		questions []events.Question
		want      string
	}{
		{
			name:      "no questions returns single-select hint",
			questions: nil,
			want:      "粘贴发送即可响应",
		},
		{
			name: "single select returns single-select hint",
			questions: []events.Question{{
				Question: "Pick?",
				Options:  []events.QuestionOption{{Label: "A"}},
			}},
			want: "粘贴发送即可响应",
		},
		{
			name: "multi select returns multi-select hint",
			questions: []events.Question{{
				Question:    "Pick?",
				MultiSelect: true,
				Options:     []events.QuestionOption{{Label: "A"}, {Label: "B"}},
			}},
			want: "可一次发送多个选项",
		},
		{
			name: "mixed questions with any multi select uses multi hint",
			questions: []events.Question{
				{Question: "Q1", Options: []events.QuestionOption{{Label: "A"}}},
				{Question: "Q2", MultiSelect: true, Options: []events.QuestionOption{{Label: "X"}}},
			},
			want: "可一次发送多个选项",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := questionFooterHint(tt.questions)
			require.Contains(t, got, tt.want)
		})
	}
}

func TestBuildCard(t *testing.T) {
	t.Parallel()
	t.Run("no header", func(t *testing.T) {
		t.Parallel()
		got := buildCard(cardHeader{}, map[string]any{"wide_screen_mode": true},
			[]map[string]any{{"tag": "markdown", "content": "hello"}})
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Equal(t, "2.0", card["schema"])
		require.Nil(t, card["header"])
	})

	t.Run("with header", func(t *testing.T) {
		t.Parallel()
		got := buildCard(cardHeader{Title: "Bot", Template: "blue"},
			map[string]any{"wide_screen_mode": true},
			[]map[string]any{{"tag": "markdown", "content": "hello"}})
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		hdr, ok := card["header"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "blue", hdr["template"])
	})
}

func TestBuildV1Card(t *testing.T) {
	t.Parallel()
	t.Run("no schema field", func(t *testing.T) {
		t.Parallel()
		got := buildV1Card(cardHeader{Title: "Test", Template: "yellow"},
			map[string]any{"wide_screen_mode": true},
			[]map[string]any{{"tag": "markdown", "content": "hello"}})
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Nil(t, card["schema"], "v1 card must not have schema field")
		require.Nil(t, card["body"], "v1 card must not have body wrapper")
		hdr, ok := card["header"].(map[string]any)
		elems := card["elements"].([]any)
		require.Len(t, elems, 1)
		require.Equal(t, "markdown", elems[0].(map[string]any)["tag"])
		require.True(t, ok)
		require.Equal(t, "yellow", hdr["template"])
	})

	t.Run("full question card round-trip", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{
				Header:   "Auth",
				Question: "Pick one",
				Options: []events.QuestionOption{
					{Label: "JWT", Description: "Token-based"},
					{Label: "OAuth"},
				},
			},
		}
		elements := buildQuestionElements(questions)
		elements = append(elements,
			map[string]any{"tag": "hr"},
			map[string]any{"tag": "markdown", "content": "回复选项"},
		)
		got := buildV1Card(cardHeader{Title: "用户输入请求", Template: "yellow"},
			map[string]any{"wide_screen_mode": true}, elements)

		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Nil(t, card["schema"])
		require.Nil(t, card["body"], "v1 card must not have body wrapper")
		elems := card["elements"].([]any)
		// markdown + action + hr + footer = 4
		require.Len(t, elems, 4)
		require.Equal(t, "markdown", elems[0].(map[string]any)["tag"])
		require.Equal(t, "action", elems[1].(map[string]any)["tag"])
		require.Equal(t, "hr", elems[2].(map[string]any)["tag"])
		require.Equal(t, "markdown", elems[3].(map[string]any)["tag"])

		// Verify action buttons have copy_text click behavior
		actionEl := elems[1].(map[string]any)
		btns := actionEl["actions"].([]any)
		require.Len(t, btns, 2)
		btn0 := btns[0].(map[string]any)
		require.Equal(t, "button", btn0["tag"])
		click := btn0["click"].(map[string]any)
		require.Equal(t, "copy_text", click["tag"])
		require.Equal(t, "JWT", click["value"])
	})
}

func TestBuildStreamingCard(t *testing.T) {
	t.Parallel()
	t.Run("no header", func(t *testing.T) {
		t.Parallel()
		got := buildStreamingCard(cardHeader{}, "summary", "content", "")
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Nil(t, card["header"])
		cfg := card["config"].(map[string]any)
		require.Equal(t, true, cfg["streaming_mode"])
	})

	t.Run("with wathet header", func(t *testing.T) {
		t.Parallel()
		got := buildStreamingCard(
			cardHeader{Title: "Bot", Subtitle: "生成中...", Template: "wathet"},
			"summary", "content", "")
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		hdr, ok := card["header"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "wathet", hdr["template"])
		sub := hdr["subtitle"].(map[string]any)
		require.Equal(t, "生成中...", sub["content"])
	})
}

func TestStringPtr(t *testing.T) {
	t.Parallel()
	p := stringPtr("test")
	require.NotNil(t, p)
	require.Equal(t, "test", *p)
}

func TestBuildQuestionElements(t *testing.T) {
	t.Parallel()

	t.Run("options without descriptions", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{
				Header:   "Auth method",
				Question: "Which library?",
				Options: []events.QuestionOption{
					{Label: "JWT"},
					{Label: "Session"},
					{Label: "OAuth"},
				},
			},
		}
		elements := buildQuestionElements(questions)

		// Expect: markdown + action
		require.Len(t, elements, 2)

		// Markdown element: always includes numbered list as fallback
		md := elements[0]
		require.Equal(t, "markdown", md["tag"])
		content := md["content"].(string)
		require.Contains(t, content, "**Auth method**")
		require.Contains(t, content, "Which library?")
		require.Contains(t, content, "1. **JWT**")
		require.Contains(t, content, "2. **Session**")
		require.Contains(t, content, "3. **OAuth**")

		// Action element: 3 buttons
		action := elements[1]
		require.Equal(t, "action", action["tag"])
		buttons := action["actions"].([]map[string]any)
		require.Len(t, buttons, 3)
		require.Equal(t, "button", buttons[0]["tag"])
		require.Equal(t, "JWT", buttons[0]["text"].(map[string]any)["content"])
		click := buttons[0]["click"].(map[string]any)
		require.Equal(t, "copy_text", click["tag"])
		require.Equal(t, "JWT", click["value"])
	})

	t.Run("options with descriptions", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{
				Header:   "Auth",
				Question: "Pick one",
				Options: []events.QuestionOption{
					{Label: "JWT", Description: "Token-based"},
					{Label: "Session", Description: "Server-side"},
				},
			},
		}
		elements := buildQuestionElements(questions)
		require.Len(t, elements, 2)

		// Markdown should include numbered list with descriptions
		md := elements[0]
		content := md["content"].(string)
		require.Contains(t, content, "1. **JWT** — Token-based")
		require.Contains(t, content, "2. **Session** — Server-side")

		// Buttons still have label only
		buttons := elements[1]["actions"].([]map[string]any)
		require.Len(t, buttons, 2)
		require.Equal(t, "JWT", buttons[0]["text"].(map[string]any)["content"])
	})

	t.Run("no options", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{Header: "Q", Question: "What?"},
		}
		elements := buildQuestionElements(questions)
		require.Len(t, elements, 1)
		require.Equal(t, "markdown", elements[0]["tag"])
	})

	t.Run("multiple questions", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{Header: "Q1", Question: "First?", Options: []events.QuestionOption{{Label: "A"}, {Label: "B"}}},
			{Header: "Q2", Question: "Second?", Options: []events.QuestionOption{{Label: "C"}}},
		}
		elements := buildQuestionElements(questions)
		// Q1: markdown + action, Q2: markdown + action = 4
		require.Len(t, elements, 4)
		require.Equal(t, "markdown", elements[0]["tag"])
		require.Equal(t, "action", elements[1]["tag"])
		require.Equal(t, "markdown", elements[2]["tag"])
		require.Equal(t, "action", elements[3]["tag"])
	})

	t.Run("empty header defaults to Question", func(t *testing.T) {
		t.Parallel()
		questions := []events.Question{
			{Question: "What?"},
		}
		elements := buildQuestionElements(questions)
		content := elements[0]["content"].(string)
		require.Contains(t, content, "**Question**")
	})

	t.Run("multi_select", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			questions []events.Question
			wantHint  string // substring expected in markdown content
			dontWant  string // substring that must NOT appear
		}{
			{
				name: "true shows multi-select marker",
				questions: []events.Question{{
					Header:      "Pick tools",
					Question:    "Which ones?",
					MultiSelect: true,
					Options:     []events.QuestionOption{{Label: "Go"}, {Label: "Rust"}},
				}},
				wantHint: "（可多选）",
			},
			{
				name: "false has no marker",
				questions: []events.Question{{
					Header:      "Pick one",
					Question:    "Which?",
					MultiSelect: false,
					Options:     []events.QuestionOption{{Label: "A"}},
				}},
				dontWant: "可多选",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				elements := buildQuestionElements(tt.questions)
				content := elements[0]["content"].(string)
				if tt.wantHint != "" {
					require.Contains(t, content, tt.wantHint)
				}
				if tt.dontWant != "" {
					require.NotContains(t, content, tt.dontWant)
				}
			})
		}
	})
}

func TestBuildPermissionCardWithButtons(t *testing.T) {
	t.Parallel()
	data := &events.PermissionRequestData{
		ID:          "perm-1",
		ToolName:    "Bash",
		Description: "Run shell command",
		Args:        []string{"ls", "-la"},
	}
	got := buildPermissionCardWithButtons(data)

	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))

	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)

	var actionEl map[string]any
	for _, e := range elements {
		m := e.(map[string]any)
		if m["tag"] == "interactive_container" {
			actionEl = m
			break
		}
	}
	require.NotNil(t, actionEl, "expected an interactive_container element")

	buttons := actionEl["elements"].([]any)
	require.Len(t, buttons, 2)

	btn0 := buttons[0].(map[string]any)
	require.Equal(t, "button", btn0["tag"])
	require.Equal(t, "primary", btn0["type"])
	require.Equal(t, "✅ 允许", btn0["text"].(map[string]any)["content"])
	val0 := btn0["value"].(map[string]any)
	require.Equal(t, "allow", val0["action"])
	require.Equal(t, "perm-1", val0["request_id"])

	btn1 := buttons[1].(map[string]any)
	require.Equal(t, "danger", btn1["type"])
	require.Equal(t, "❌ 拒绝", btn1["text"].(map[string]any)["content"])
	val1 := btn1["value"].(map[string]any)
	require.Equal(t, "deny", val1["action"])
	require.Equal(t, "perm-1", val1["request_id"])

	hdr := card["header"].(map[string]any)
	require.Equal(t, "orange", hdr["template"])
}

func TestBuildPermissionCardWithButtons_NoArgs(t *testing.T) {
	t.Parallel()
	data := &events.PermissionRequestData{
		ID:          "perm-2",
		ToolName:    "Read",
		Description: "Read file",
		Args:        nil,
	}
	got := buildPermissionCardWithButtons(data)

	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))

	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	require.GreaterOrEqual(t, len(elements), 2)

	md := elements[0].(map[string]any)
	require.Equal(t, "markdown", md["tag"])
	content := md["content"].(string)
	require.Contains(t, content, "Read")
	require.NotContains(t, content, "参数：")

	var actionEl map[string]any
	for _, e := range elements {
		m := e.(map[string]any)
		if m["tag"] == "interactive_container" {
			actionEl = m
			break
		}
	}
	require.NotNil(t, actionEl)
	buttons := actionEl["elements"].([]any)
	require.Len(t, buttons, 2)
}

func TestBuildQuestionCardWithButtons(t *testing.T) {
	t.Parallel()
	data := &events.QuestionRequestData{
		ID:       "q-1",
		ToolName: "AskUserQuestion",
		Questions: []events.Question{
			{
				Header:   "Auth",
				Question: "Pick one",
				Options: []events.QuestionOption{
					{Label: "JWT"},
					{Label: "OAuth"},
				},
			},
		},
	}
	got := buildQuestionCardWithButtons(data)

	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))
	require.Equal(t, "2.0", card["schema"], "question card should now use v2 format")

	body := card["body"].(map[string]any)
	elems := body["elements"].([]any)

	var actionEl map[string]any
	for _, e := range elems {
		m := e.(map[string]any)
		if m["tag"] == "interactive_container" {
			actionEl = m
			break
		}
	}
	require.NotNil(t, actionEl, "expected an interactive_container element")

	buttons := actionEl["elements"].([]any)
	require.Len(t, buttons, 2)

	for _, b := range buttons {
		btn := b.(map[string]any)
		require.Equal(t, "button", btn["tag"])
		val := btn["value"].(map[string]any)
		require.Equal(t, "answer", val["action"])
		require.Equal(t, "q-1", val["request_id"])
		require.NotEmpty(t, val["answer"])
		require.NotEmpty(t, val["label"])
	}

	hdr := card["header"].(map[string]any)
	require.Equal(t, "yellow", hdr["template"])
}

func TestBuildQuestionCardWithButtons_NoOptions(t *testing.T) {
	t.Parallel()
	data := &events.QuestionRequestData{
		ID:       "q-2",
		ToolName: "AskUserQuestion",
		Questions: []events.Question{
			{Header: "Open", Question: "Why?"},
		},
	}
	got := buildQuestionCardWithButtons(data)

	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))

	body := card["body"].(map[string]any)
	elems := body["elements"].([]any)
	for _, e := range elems {
		m := e.(map[string]any)
		require.NotEqual(t, "interactive_container", m["tag"], "no interactive_container element expected when question has no options")
	}
}

func TestBuildElicitationCardWithButtons(t *testing.T) {
	t.Parallel()
	data := &events.ElicitationRequestData{
		ID:            "e-1",
		MCPServerName: "github",
		Message:       "Allow access",
		URL:           "https://github.com/login/oauth",
	}
	got := buildElicitationCardWithButtons(data)

	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))

	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)

	md := elements[0].(map[string]any)
	require.Equal(t, "markdown", md["tag"])
	content := md["content"].(string)
	require.Contains(t, content, "https://github.com/login/oauth", "URL must appear as link in body")

	var actionEl map[string]any
	var inputEl map[string]any
	for _, e := range elements {
		m := e.(map[string]any)
		switch m["tag"] {
		case "interactive_container":
			actionEl = m
		case "input":
			inputEl = m
		}
	}
	require.NotNil(t, inputEl, "expected an input element")
	require.Equal(t, "comment", inputEl["name"])
	require.NotNil(t, actionEl, "expected an interactive_container element")

	buttons := actionEl["elements"].([]any)
	require.Len(t, buttons, 2)

	btn0 := buttons[0].(map[string]any)
	require.Equal(t, "primary", btn0["type"])
	require.Equal(t, "✅ 接受", btn0["text"].(map[string]any)["content"])
	val0 := btn0["value"].(map[string]any)
	require.Equal(t, "accept", val0["action"])
	require.Equal(t, "e-1", val0["request_id"])

	btn1 := buttons[1].(map[string]any)
	require.Equal(t, "danger", btn1["type"])
	require.Equal(t, "❌ 拒绝", btn1["text"].(map[string]any)["content"])
	val1 := btn1["value"].(map[string]any)
	require.Equal(t, "decline", val1["action"])
	require.Equal(t, "e-1", val1["request_id"])

	hdr := card["header"].(map[string]any)
	require.Equal(t, "violet", hdr["template"])
}

func TestBuildResolvedCard_Green(t *testing.T) {
	t.Parallel()
	card := buildResolvedCard("allow", "已允许", "", "test_summary", "ou_123", "")
	require.NotNil(t, card)

	hdr := card["header"].(map[string]any)
	require.Equal(t, "green", hdr["template"])
	title := hdr["title"].(map[string]any)
	require.Equal(t, "plain_text", title["tag"])
	require.Equal(t, "已允许", title["content"])
}

func TestBuildResolvedCard_Red(t *testing.T) {
	t.Parallel()
	card := buildResolvedCard("deny", "已拒绝", "", "test_summary", "", "")
	hdr := card["header"].(map[string]any)
	require.Equal(t, "red", hdr["template"])
}

func TestBuildResolvedCard_ExplicitColor(t *testing.T) {
	t.Parallel()
	card := buildResolvedCard("allow", "Custom Label", "blue", "", "", "")
	hdr := card["header"].(map[string]any)
	require.Equal(t, "blue", hdr["template"])
	title := hdr["title"].(map[string]any)
	require.Equal(t, "Custom Label", title["content"])
}
