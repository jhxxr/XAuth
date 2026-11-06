package outlook

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAccountsText(t *testing.T) {
	input := `
# comment line should be ignored
MollyPennington8418@outlook.com----hwqennm1840830----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-token1
nopass@hotmail.com--------dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-token2
threepart@outlook.com----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-token3
bad-line-without-delimiters
invalid-email----pwd----cid----rt
`
	accounts, errs := ParseAccountsText(input)
	if len(accounts) != 3 {
		t.Fatalf("expected 3 valid accounts, got %d (%+v)", len(accounts), accounts)
	}
	if len(errs) != 2 {
		t.Fatalf("expected 2 parse errors, got %d (%+v)", len(errs), errs)
	}
	if accounts[0].Email != "mollypennington8418@outlook.com" || accounts[0].Password != "hwqennm1840830" {
		t.Fatalf("unexpected first account: %+v", accounts[0])
	}
	if accounts[1].Email != "nopass@hotmail.com" || accounts[1].Password != "" {
		t.Fatalf("unexpected second account: %+v", accounts[1])
	}
	if accounts[2].Email != "threepart@outlook.com" || accounts[2].RefreshToken != "M.C559_BAY.0.U.-token3" {
		t.Fatalf("unexpected third account: %+v", accounts[2])
	}
	formatted := FormatAccountLine(accounts[0].Email, accounts[0].Password, accounts[0].ClientID, accounts[0].RefreshToken)
	reparsed, err := ParseAccountLine(formatted)
	if err != nil || reparsed.Email != accounts[0].Email || reparsed.RefreshToken != accounts[0].RefreshToken {
		t.Fatalf("round-trip failed: %v %+v", err, reparsed)
	}

	// Graph message IDs containing "/" or "+" should round-trip safely via g64:
	slashID := "AAMkAGI2/THVS+AAA="
	prefixed := prefixForMethod(MethodGraph, slashID)
	if strings.ContainsAny(prefixed, "/+") {
		t.Fatalf("prefixed ID %q still contains unsafe path characters", prefixed)
	}
	m, raw := splitMessageID(prefixed)
	if m != MethodGraph || raw != slashID {
		t.Fatalf("splitMessageID(%q) = (%q, %q), want (%q, %q)", prefixed, m, raw, MethodGraph, slashID)
	}
}

func TestExtractCode(t *testing.T) {
	cases := []struct {
		subject string
		text    string
		want    string
	}{
		{"Microsoft 帐户安全代码", "请使用以下安全代码验证您的身份：482910。谢谢（2026 年）。", "482910"},
		{"Your verification code", "Your verification code is: 739201", "739201"},
		{"Login notification", "839204 是您的动态码，请勿泄露。", "839204"},
		{"Weekly newsletter 2026", "Welcome to our 2026 update!", ""},
	}
	for _, tc := range cases {
		got := ExtractCode(tc.subject, tc.text, "")
		if got != tc.want {
			t.Errorf("ExtractCode(%q, %q) = %q, want %q", tc.subject, tc.text, got, tc.want)
		}
	}
}

func TestGraphAndIMAPFallback(t *testing.T) {
	// 1. Mock token + Graph server
	graphFail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token/graph":
			if graphFail {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "graph-tok-123",
				"expires_in":   3600,
			})
		case "/token/imap":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "imap-tok-456",
				"expires_in":   3600,
			})
		case "/v1.0/me/mailFolders/inbox/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":               "msg-1",
						"subject":          "验证码通知",
						"from":             map[string]any{"emailAddress": map[string]any{"name": "Microsoft", "address": "account-security-noreply@accountprotection.microsoft.com"}},
						"receivedDateTime": "2026-10-30T12:00:00Z",
						"isRead":           false,
						"bodyPreview":      "您的安全代码为 918273，请在 10 分钟内输入。",
					},
				},
			})
		case "/v1.0/me/messages/msg-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":               "msg-1",
				"subject":          "验证码通知",
				"from":             map[string]any{"emailAddress": map[string]any{"name": "Microsoft", "address": "account-security-noreply@accountprotection.microsoft.com"}},
				"receivedDateTime": "2026-10-30T12:00:00Z",
				"isRead":           true,
				"bodyPreview":      "您的安全代码为 918273",
				"body": map[string]any{
					"contentType": "html",
					"content":     "<p>您的安全代码为 <b>918273</b></p>",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// 2. Mock TLS IMAP server using httptest TLS cert
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	tlsCert := tlsSrv.TLS.Certificates
	tlsClientCfg := tlsSrv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsSrv.Close()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: tlsCert})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer ln.Close()

	rawMsg := "From: GitHub <noreply@github.com>\r\n" +
		"To: test@outlook.com\r\n" +
		"Subject: [GitHub] Please verify your device\r\n" +
		"Date: Fri, 30 Oct 2026 12:05:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Verification code: 654321\r\n"

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = fmt.Fprintf(conn, "* OK Outlook IMAP4rev1 Server Ready\r\n")
				br := bufio.NewReader(conn)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					parts := strings.Fields(line)
					if len(parts) < 2 {
						continue
					}
					tag, cmd := parts[0], strings.ToUpper(parts[1])
					switch cmd {
					case "AUTHENTICATE":
						_, _ = fmt.Fprintf(conn, "+ \r\n")
						_, _ = br.ReadString('\n')
						_, _ = fmt.Fprintf(conn, "%s OK AUTHENTICATE completed.\r\n", tag)
					case "SELECT":
						_, _ = fmt.Fprintf(conn, "* 1 EXISTS\r\n%s OK [READ-WRITE] SELECT completed.\r\n", tag)
					case "SEARCH":
						_, _ = fmt.Fprintf(conn, "* SEARCH 1\r\n%s OK SEARCH completed.\r\n", tag)
					case "FETCH":
						_, _ = fmt.Fprintf(conn, "* 1 FETCH (BODY[] {%d}\r\n%s)\r\n%s OK FETCH completed.\r\n", len(rawMsg), rawMsg, tag)
					case "LOGOUT":
						_, _ = fmt.Fprintf(conn, "* BYE IMAP4rev1 Server logging out\r\n%s OK LOGOUT completed.\r\n", tag)
						return
					}
				}
			}()
		}
	}()

	client := NewClient()
	client.TokenURLGraph = srv.URL + "/token/graph"
	client.TokenURLIMAP = srv.URL + "/token/imap"
	client.GraphBaseURL = srv.URL + "/v1.0"
	client.IMAPServerNew = ln.Addr().String()
	client.TLSConfig = tlsClientCfg

	creds := Credentials{
		Email:        "test@outlook.com",
		ClientID:     "cid-1",
		RefreshToken: "rt-1",
	}

	// Test 1: Auto mode succeeds via Graph API
	items, usedMethod, err := client.ListMessages(context.Background(), creds, MethodAuto, 10)
	if err != nil {
		t.Fatalf("ListMessages (Graph): %v", err)
	}
	if usedMethod != MethodGraph || len(items) != 1 {
		t.Fatalf("unexpected method=%s items=%+v", usedMethod, items)
	}
	if items[0].VerificationCode == nil || *items[0].VerificationCode != "918273" {
		t.Fatalf("expected code 918273, got %+v", items[0].VerificationCode)
	}

	detail, err := client.GetMessage(context.Background(), creds, items[0].ID, MethodAuto)
	if err != nil || !detail.HasHTML {
		t.Fatalf("GetMessage (Graph): err=%v detail=%+v", err, detail)
	}

	// Test 2: When Graph fails, Auto mode falls back to IMAP
	graphFail = true
	client.tokens = map[string]cachedToken{}
	items, usedMethod, err = client.ListMessages(context.Background(), creds, MethodAuto, 10)
	if err != nil {
		t.Fatalf("ListMessages (IMAP fallback): %v", err)
	}
	if usedMethod != MethodIMAPNew || len(items) != 1 {
		t.Fatalf("expected imap_new with 1 message, got method=%s items=%+v", usedMethod, items)
	}
	if items[0].VerificationCode == nil || *items[0].VerificationCode != "654321" {
		t.Fatalf("expected code 654321 from IMAP, got %+v", items[0].VerificationCode)
	}
}
