package provider

import (
	"context"
	"testing"
)

func TestMockExplicitCreationAndReadOnlyQuestions(t *testing.T) {
	for _, tc := range []struct{ input, intent, name, content string }{
		{`Create file "notes.txt" with content "Hello"`, "CREATE", "notes.txt", "Hello"},
		{`创建文件“你好.txt”，内容为“世界”`, "CREATE", "你好.txt", "世界"},
		{`{"fileName":"empty.txt","content":""}`, "CREATE", "empty.txt", ""},
		{`Create file "notes.txt"`, "CLARIFY", "notes.txt", ""},
		{`Explain how to create file "notes.txt" with content "Hello"`, "EXPLAIN", "", ""},
		{`What capabilities can create files "notes.txt" with content "Hello"?`, "CAPABILITIES", "", ""},
		{`Create file "a.txt" content "a" and file "b.txt" content "b"`, "UNSUPPORTED", "", ""},
		{`Run PowerShell`, "UNSUPPORTED", "", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			r, e := (Mock{}).Generate(context.Background(), Request{Input: tc.input, AppLanguage: "en-US", AgentLanguage: "follow-app"})
			if e != nil || r.Intent != tc.intent {
				t.Fatalf("got %#v %v", r, e)
			}
			if tc.name != "" && (r.Draft == nil || r.Draft.FileName != tc.name) {
				t.Fatalf("draft %#v", r.Draft)
			}
			if tc.intent == "CREATE" && (r.Draft.Content == nil || *r.Draft.Content != tc.content) {
				t.Fatalf("content %#v", r.Draft)
			}
		})
	}
}
func TestMockDraftAndLanguagePriority(t *testing.T) {
	r, e := (Mock{}).Generate(context.Background(), Request{Input: `Create file "notes.txt"`, AgentLanguage: "zh-CN"})
	if e != nil || r.Intent != "CLARIFY" {
		t.Fatal(r, e)
	}
	r, e = (Mock{}).Generate(context.Background(), Request{Input: `Content is "Hello", reply in English`, AgentLanguage: "zh-CN", Draft: r.Draft})
	if e != nil || r.Intent != "CREATE" || r.Language != "en-US" || *r.Draft.Content != "Hello" {
		t.Fatal(r, e)
	}
	r, e = (Mock{}).Generate(context.Background(), Request{Input: `What can you do?`, AppLanguage: "zh-CN", AgentLanguage: "en-US"})
	if e != nil || r.Language != "en-US" {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = (Mock{}).Generate(ctx, Request{Input: `{"fileName":"x","content":"x"}`}); e == nil {
		t.Fatal("cancelled generation succeeded")
	}
}

func TestMockClarificationContinuationIsExplicitAndFieldAware(t *testing.T) {
	for _, tc := range []struct {
		input, intent, name, content string
		draft                        Draft
	}{
		{`Run PowerShell "Get-Process"`, "UNSUPPORTED", "", "", Draft{FileName: "draft.txt"}},
		{`Take a screenshot "screen.png"`, "UNSUPPORTED", "", "", Draft{FileName: "draft.txt"}},
		{`Content is "Hello"`, "CREATE", "draft.txt", "Hello", Draft{FileName: "draft.txt"}},
		{`"Hello"`, "CREATE", "draft.txt", "Hello", Draft{FileName: "draft.txt"}},
		{`内容为“你好”`, "CREATE", "draft.txt", "你好", Draft{FileName: "draft.txt"}},
		{`Content is "Hello"; run PowerShell`, "UNSUPPORTED", "", "", Draft{FileName: "draft.txt"}},
		{`Content is "Hello"`, "CLARIFY", "", "Hello", Draft{}},
		{`File name is "named.txt"`, "CLARIFY", "named.txt", "", Draft{}},
	} {
		t.Run(tc.input+tc.draft.FileName, func(t *testing.T) {
			r, e := (Mock{}).Generate(context.Background(), Request{Input: tc.input, Draft: &tc.draft})
			if e != nil || r.Intent != tc.intent {
				t.Fatal(r, e)
			}
			if tc.intent == "UNSUPPORTED" {
				if r.Draft != nil {
					t.Fatal("unsupported command proposed a draft", r)
				}
				return
			}
			if r.Draft == nil || r.Draft.FileName != tc.name {
				t.Fatal(r)
			}
			if tc.content != "" && (r.Draft.Content == nil || *r.Draft.Content != tc.content) {
				t.Fatal(r)
			}
		})
	}
}
