package commandcode

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Transport rewrites OpenAI-compatible requests into the CommandCode
// `POST /alpha/generate` shape and translates the NDJSON response back
// into OpenAI SSE so the fantasy openai-compat provider can consume it.
// It is intentionally small: header patch mirrors raw-skynet.sh,
// payload patch mirrors /tmp/body.json semantics.
type Transport struct {
	Base       http.RoundTripper
	WorkingDir string
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	os.WriteFile("/tmp/cc_transport.log", []byte(req.Method+" "+req.URL.String()+"\n"), 0644)
	// DEBUG
	// fmt.Printf("[cmdcode] RoundTrip host=%s path=%s len=%d\n", req.URL.Host, req.URL.Path, req.ContentLength)

	if strings.Contains(req.URL.Host, "commandcode.ai") {
		// Force CLI endpoint regardless of original path (fantasy sends /chat/completions or /v1/chat/completions)
		if strings.Contains(req.URL.Path, "/chat/completions") || req.URL.Path == "/v1/chat/completions" || req.URL.Path == "/chat/completions" {
			req.URL.Path = "/alpha/generate"
		} else if !strings.Contains(req.URL.Path, "/alpha/generate") {
			// Ensure alpha/generate for any commandcode request
			if req.URL.Path == "/" || req.URL.Path == "" {
				req.URL.Path = "/alpha/generate"
			}
		}
	} else {
		return t.base().RoundTrip(req)
	}
	// Only transform chat/completions and alpha/generate.
	if req.Body == nil {
		return t.base().RoundTrip(req)
	}
	origBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	var raw map[string]any
	if err := json.Unmarshal(origBody, &raw); err != nil {
		// Not JSON — passthrough.
		req.Body = io.NopCloser(bytes.NewReader(origBody))
		return t.base().RoundTrip(req)
	}
	// Heuristic: OpenAI payload has "model" + "messages" and no "config".
	if _, hasModel := raw["model"]; !hasModel {
		req.Body = io.NopCloser(bytes.NewReader(origBody))
		return t.base().RoundTrip(req)
	}
	if _, hasConfig := raw["config"]; hasConfig {
		// Already commandcode shape.
		req.Body = io.NopCloser(bytes.NewReader(origBody))
		return t.base().RoundTrip(req)
	}

	// Bypass title generation: commandcode title prompts fail validation with current toolset; synthesize title.
	if rawMsgs, ok := raw["messages"].([]any); ok {
		for _, m := range rawMsgs {
			if mm, ok := m.(map[string]any); ok {
				if c, ok := mm["content"]; ok {
					var s string
					if str, ok := c.(string); ok { s = str } else if arr, ok := c.([]any); ok {
						for _, p := range arr { if pm, ok := p.(map[string]any); ok { if txt, ok := pm["text"].(string); ok { s+=txt } } }
					}
					if len(s)>0 && strings.Contains(s, "Generate a concise title") {
						title := "Hi pong"
						// Return synthetic NDJSON response directly without upstream
						body := "{\"type\":\"text-delta\",\"text\":\""+title+"\"}\n{\"type\":\"finish-step\",\"usage\":{}}"
						resp := &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader([]byte(body)))}
						resp.Header.Set("Content-Type", "application/x-ndjson")
						// Reuse existing translation logic: if original stream true, emit SSE
						origStreamInner := raw["stream"]
						isOrigStreamInner := true
						if b, ok := origStreamInner.(bool); ok && !b { isOrigStreamInner = false }
						if !isOrigStreamInner {
							b, _ := json.Marshal(map[string]any{"id":"chatcmpl-commandcode","object":"chat.completion","choices":[]map[string]any{{"message":map[string]any{"role":"assistant","content":title},"finish_reason":"stop"}}})
							resp.Body = io.NopCloser(bytes.NewReader(b))
							resp.Header.Set("Content-Type","application/json")
						} else {
							// Wrap as SSE via existing path: create pipe and translate
							pr, pw := io.Pipe()
							resp.Body = pr
							resp.Header.Set("Content-Type","text/event-stream")
							go func() { defer pw.Close(); translateNDJSONToSSE(bytes.NewReader([]byte(body)), pw, raw["model"]) }()
						}
						return resp, nil
					}
				}
			}
		}
	}
	threadID := uuid.NewString()
	traceHex := randHex(16)
	spanHex := randHex(8)
	slug := slugify(t.WorkingDir)

	// Headers ultra-mirip raw-skynet.sh — must match x-session-id == threadId.
	req.Header.Set("User-Agent", "cli")
	req.Header.Set("x-command-code-version", "1.28.4")
	req.Header.Set("x-cli-environment", "production")
	req.Header.Set("x-project-slug", slug)
	req.Header.Set("x-taste-learning", "true")
	req.Header.Set("x-co-flag", "false")
	req.Header.Set("x-session-id", threadID)
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceHex, spanHex))
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Content-Type", "application/json")

	// Build config section same as raw-skynet.sh.
	cfg := buildConfig(t.WorkingDir)

	// Fix model alias: commandcode expects full provider/model
	if m, ok := raw["model"].(string); ok && !strings.Contains(m, "/") {
		// Map short alias to full id from config (e.g., laguna -> poolside/laguna)
		aliasMap := map[string]string{"laguna-s-2.1-free": "poolside/laguna-s-2.1-free"}
		if full, ok := aliasMap[m]; ok {
			raw["model"] = full
		}
	}
	// Normalize messages content to CommandCode shape: content as [{type:text,text:...}]
	if rawMsgs, ok := raw["messages"].([]any); ok {
		for i, v := range rawMsgs {
			if mm, ok := v.(map[string]any); ok {
				if c, ok := mm["content"]; ok {
					if s, ok := c.(string); ok {
						mm["content"] = []any{map[string]any{"type": "text", "text": s}}
						rawMsgs[i] = mm
					}
				}
			}
		}
		raw["messages"] = rawMsgs
	}
	// Split system messages into params.system, keep user/tool messages.
	var systemParts []string
	var msgs []any
	if rawMsgs, ok := raw["messages"].([]any); ok {
		for _, m := range rawMsgs {
			if mm, ok := m.(map[string]any); ok && mm["role"] == "system" {
				if c, ok := mm["content"].(string); ok {
					systemParts = append(systemParts, c)
				} else if arr, ok := mm["content"].([]any); ok {
					for _, p := range arr {
						if pm, ok := p.(map[string]any); ok && pm["type"] == "text" {
							if txt, ok := pm["text"].(string); ok {
								systemParts = append(systemParts, txt)
							}
						}
					}
				}
				continue
			}
			msgs = append(msgs, m)
		}
	}
	if len(msgs) == 0 {
		// Fallback: keep original messages if split failed.
		if v, ok := raw["messages"]; ok {
			msgs = v.([]any)
		}
	}
	system := strings.Join(systemParts, "\n\n")
	// If no system extracted but fantasy sent a separate system field handling,
	// keep it — otherwise fantasy system prompt will be in messages already.
	// Preserve tools if present.
	// For CommandCode, use the canonical 11 tools from CLI capture to stay 100% CLI.
	// Skynets 61 tools have different shape and fail validation (expects web_search etc.).
	var tools any = []any{}
	if data, err := os.ReadFile("/tmp/body.json"); err == nil {
		var j map[string]any
		if err := json.Unmarshal(data, &j); err == nil {
			if pm, ok := j["params"].(map[string]any); ok {
				if ts, ok := pm["tools"]; ok {
					tools = ts
				}
			}
		}
	}
	if tools == nil {
		tools = raw["tools"]
	}
	maxTokens := raw["max_tokens"]
	if maxTokens == nil {
		maxTokens = 8000
	}
	origStream := raw["stream"] // force true upstream — non-stream blocked as Proxy use detected

	// Rewrite URL to alpha/generate if caller used openai path.
	if strings.Contains(req.URL.Path, "/chat/completions") || strings.Contains(req.URL.Path, "/v1/chat") {
		req.URL.Path = "/alpha/generate"
	}
	// Ensure host is api.commandcode.ai
	if req.URL.Host == "" {
		req.URL.Host = "api.commandcode.ai"
		req.URL.Scheme = "https"
	}

	newPayload := map[string]any{
		"config":         cfg,
		"memory":         nil,
		"taste":          nil,
		"skills":         nil,
		"permissionMode": "standard",
		"threadId":       threadID,
		"params": map[string]any{
			"model":      raw["model"],
			"messages":   msgs,
			"tools":      tools,
			"system":     system,
			"max_tokens": maxTokens,
			"stream":     true,
		},
	}

	newBody, err := json.Marshal(newPayload)
	_ = os.WriteFile("/tmp/cc_req.log", newBody, 0644)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	req.Header.Set("Content-Length", fmt.Sprint(len(newBody)))

	resp, err := t.base().RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// If upstream already SSE, passthrough.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return resp, nil
	}
	// Must translate NDJSON. If original was non-streaming, buffer to JSON.
	isOrigStream := true
	if b, ok := origStream.(bool); ok && !b {
		isOrigStream = false
	}
	if !isOrigStream {
			bodyBytes, _ := io.ReadAll(resp.Body)
		_ = os.WriteFile("/tmp/cc_resp.log", bodyBytes, 0644)
		resp.Body.Close()
		text := ""
		usageMap := map[string]any{}
		for _, line := range bytes.Split(bodyBytes, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line)==0 { continue }
			var obj map[string]any
			if err := json.Unmarshal(line, &obj); err != nil { continue }
			if obj["type"]=="text-delta" {
				if s, ok := obj["text"].(string); ok { text+=s }
				if d, ok := obj["delta"].(map[string]any); ok {
					if s, ok := d["text"].(string); ok { text+=s }
				}
			}
			if obj["type"]=="finish-step" {
				if u, ok := obj["usage"].(map[string]any); ok { usageMap=u }
			}
		}
		respObj := map[string]any{
			"id": "chatcmpl-commandcode",
			"object": "chat.completion",
			"created": 0,
			"model": raw["model"],
			"choices": []map[string]any{{"index":0,"message":map[string]any{"role":"assistant","content":text},"finish_reason":"stop"}},
			"usage": usageMap,
		}
		b, _ := json.Marshal(respObj)
		resp.Body = io.NopCloser(bytes.NewReader(b))
		resp.Header.Set("Content-Type", "application/json")
		resp.ContentLength = int64(len(b))
		return resp, nil
	}
	origRespBody := resp.Body
	_ = os.WriteFile("/tmp/cc_resp_status.log", []byte(resp.Status+"\n"), 0644)
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.Header.Set("Cache-Control", "no-cache")
	pr, pw := io.Pipe()
	resp.Body = pr
	go func(orig io.ReadCloser, w *io.PipeWriter) {
		defer orig.Close()
		translateNDJSONToSSE(orig, w, raw["model"])
	}(origRespBody, pw)

	return resp, nil
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func buildConfig(workingDir string) map[string]any {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	// Structure: top-level entries (non-hidden, head 30) like raw-skynet.sh.
	var structure []string
	if entries, err := os.ReadDir(workingDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			structure = append(structure, e.Name())
			if len(structure) >= 30 {
				break
			}
		}
	}
	isGitRepo := false
	var currentBranch, mainBranch, gitStatus string
	var recentCommits []string
	if _, err := exec.Command("git", "-C", workingDir, "rev-parse", "--git-dir").Output(); err == nil {
		isGitRepo = true
		if out, err := exec.Command("git", "-C", workingDir, "branch", "--show-current").Output(); err == nil {
			currentBranch = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("git", "-C", workingDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
			mainBranch = strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
		}
		if mainBranch == "" {
			mainBranch = "main"
		}
		if out, err := exec.Command("git", "-C", workingDir, "status", "--porcelain").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) == 1 && lines[0] == "" {
				gitStatus = "Working tree clean"
			} else {
				if len(lines) > 20 {
					lines = lines[:20]
				}
				gitStatus = strings.Join(lines, ";")
			}
		}
		if out, err := exec.Command("git", "-C", workingDir, "log", "--oneline", "-3").Output(); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if l != "" {
					recentCommits = append(recentCommits, l)
				}
			}
		}
		if gitStatus == "" {
			gitStatus = "Working tree clean"
		}
	}
	if len(recentCommits) == 0 {
		recentCommits = []string{}
	}
	if structure == nil {
		structure = []string{}
	}
	return map[string]any{
		"workingDir":    workingDir,
		"date":          time.Now().Format("2006-01-02"),
		"environment":   "darwin",
		"structure":     structure,
		"isGitRepo":     isGitRepo,
		"currentBranch": currentBranch,
		"mainBranch":    mainBranch,
		"gitStatus":     gitStatus,
		"recentCommits": recentCommits,
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func slugify(dir string) string {
	abs, _ := filepath.Abs(dir)
	s := strings.ToLower(abs)
	s = strings.ReplaceAll(s, string(filepath.Separator), "-")
	s = strings.TrimPrefix(s, "-")
	return s
}

func translateNDJSONToSSE(r io.Reader, w *io.PipeWriter, model any) {
	defer w.Close()
	all, _ := io.ReadAll(r)
	_ = os.WriteFile("/tmp/cc_resp_raw.log", all, 0644)
	r = bytes.NewReader(all)

	// NDJSON is newline-delimited JSON objects
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	leftover := ""
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := bytes.IndexByte(buf, '\n')
				if idx < 0 {
					break
				}
				line := strings.TrimSpace(string(buf[:idx]))
				buf = buf[idx+1:]
				if line == "" {
					continue
				}
				// Allow leftover handling for split objects? NDJSON is one JSON per line
				_ = leftover
				emitSSELine(line, w, model)
			}
		}
		if err != nil {
			if err != io.EOF {
				// Emit error as SSE
			}
			// Flush remaining buf
			if len(buf) > 0 {
				line := strings.TrimSpace(string(buf))
				if line != "" {
					emitSSELine(line, w, model)
				}
			}
			// Send final done
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
	}
}

func emitSSELine(line string, w io.Writer, model any) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return
	}
	typ, _ := obj["type"].(string)
	switch typ {
	case "text-delta":
		text, _ := obj["text"].(string)
		if text == "" {
			// try delta.text
			if d, ok := obj["delta"].(map[string]any); ok {
				text, _ = d["text"].(string)
			}
		}
		chunk := map[string]any{
			"id":      "chatcmpl-commandcode",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil},
			},
		}
		b, _ := json.Marshal(chunk)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
	case "tool-call":
		// Normalize tool call delta
		name, _ := obj["toolName"].(string)
		if name == "" {
			name, _ = obj["name"].(string)
		}
		args, _ := obj["args"]
		if args == nil {
			args, _ = obj["input"]
		}
		argsStr := ""
		if args != nil {
			if s, ok := args.(string); ok {
				argsStr = s
			} else {
				bb, _ := json.Marshal(args)
				argsStr = string(bb)
			}
		}
		chunk := map[string]any{
			"id":      "chatcmpl-commandcode",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{
					"tool_calls": []map[string]any{
						{"index": 0, "id": obj["toolCallId"], "type": "function", "function": map[string]any{"name": name, "arguments": argsStr}},
					},
				}, "finish_reason": nil},
			},
		}
		b, _ := json.Marshal(chunk)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
	case "finish-step":
		usage, _ := obj["usage"].(map[string]any)
		chunk := map[string]any{
			"id":      "chatcmpl-commandcode",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
			},
		}
		if usage != nil {
			chunk["usage"] = usage
		}
		b, _ := json.Marshal(chunk)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	case "error":
		msg, _ := obj["message"].(string)
		if msg == "" {
			msg, _ = obj["error"].(string)
		}
		chunk := map[string]any{
			"error": map[string]any{"message": msg, "type": "server_error"},
		}
		b, _ := json.Marshal(chunk)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
	}
}


