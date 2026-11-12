package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// Charged requests can be refused or rejected locally. Preserve only usage the
// provider actually returned, even though text/tool outcome admission failed.
func TestFailureRetainsReportedUsage(t *testing.T) {
	cases := []struct { name, protocol, body, code string; stream bool }{
		{"responses-refusal",Responses,`{"status":"completed","usage":{"input_tokens":8,"output_tokens":2},"output":[{"type":"message","content":[{"type":"refusal","refusal":"declined"}]}],"message":"raw-error-marker"}`,"PROVIDER_REFUSAL",false},
		{"responses-tool",Responses,`{"status":"completed","usage":{"input_tokens":8,"output_tokens":2},"output":[{"type":"function_call","arguments":"raw-error-marker"}]}`,"UNSUPPORTED_TOOL_CALL",false},
		{"chat-refusal",ChatCompletions,`{"usage":{"prompt_tokens":8,"completion_tokens":2},"choices":[{"index":0,"message":{"refusal":"declined"},"finish_reason":"stop"}],"message":"raw-error-marker"}`,"PROVIDER_REFUSAL",false},
		{"chat-tool",ChatCompletions,`{"usage":{"prompt_tokens":8,"completion_tokens":2},"choices":[{"index":0,"message":{"tool_calls":[{"type":"function","function":{"name":"shell","arguments":"raw-error-marker"}}]},"finish_reason":"tool_calls"}]}`,"UNSUPPORTED_TOOL_CALL",false},
		{"responses-stream-refusal",Responses,`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":2},"output":[{"type":"message","content":[{"type":"refusal","refusal":"declined"}]}]},"message":"raw-error-marker"}`,"PROVIDER_REFUSAL",true},
		{"responses-stream-tool",Responses,`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":2},"output":[{"type":"shell_call","arguments":"raw-error-marker"}]}}`,"UNSUPPORTED_TOOL_CALL",true},
		{"chat-stream-refusal",ChatCompletions,`{"usage":{"prompt_tokens":8,"completion_tokens":2},"choices":[{"index":0,"delta":{"refusal":"declined"}}],"message":"raw-error-marker"}`,"PROVIDER_REFUSAL",true},
		{"chat-stream-tool",ChatCompletions,`{"usage":{"prompt_tokens":8,"completion_tokens":2},"choices":[{"index":0,"delta":{"tool_calls":[{"type":"function","function":{"name":"shell","arguments":"raw-error-marker"}}]}}]}`,"UNSUPPORTED_TOOL_CALL",true},
		{"responses-incomplete",Responses,`{"status":"incomplete","usage":{"input_tokens":8,"output_tokens":2},"output":[]}`,"RESPONSE_INCOMPLETE",false},
		{"chat-incomplete",ChatCompletions,`{"usage":{"prompt_tokens":8,"completion_tokens":2},"choices":[{"index":0,"message":{"content":"partial"},"finish_reason":"length"}]}`,"RESPONSE_INCOMPLETE",false},
	}
	for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
		var requests atomic.Int32
		c,_:=clientFixture(t,tc.protocol,func(w http.ResponseWriter,r *http.Request){
			requests.Add(1)
			if tc.stream {w.Header().Set("Content-Type","text/event-stream");fmt.Fprintf(w,"data: %s\n\n",tc.body)}else{fmt.Fprint(w,tc.body)}
		})
		defer c.Close()
		var out Response;var err error
		if tc.stream {out,err=c.Stream(context.Background(),fixtureRequest(),nil,nil)}else{out,err=c.Generate(context.Background(),fixtureRequest(),nil)}
		assertCode(t,err,tc.code)
		if out.Status!="FAILED"||out.ErrorCode!=tc.code||requests.Load()!=1 {t.Fatalf("unexpected outcome %+v requests=%d",out,requests.Load())}
		u:=out.Usage
		if !u.UsageAvailable||u.Source!="PROVIDER_REPORTED"||u.InputTokens==nil||*u.InputTokens!=8||u.OutputTokens==nil||*u.OutputTokens!=2 {t.Fatalf("lost charged usage %+v",u)}
		if u.TotalTokens!=nil||u.CachedInputTokens!=nil||u.ReasoningTokens!=nil {t.Fatalf("fabricated unavailable usage %+v",u)}
		b,_:=json.Marshal(out)
		if strings.Contains(string(b),"raw-error-marker")||strings.Contains(err.Error(),"raw-error-marker") {t.Fatal("provider error/tool payload disclosed")}
	})}
}
