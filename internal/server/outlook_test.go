package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xauth/internal/outlook"
)

func TestOutlookBatchImportAssignAndUserAccess(t *testing.T) {
	fakeMS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/token/graph":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "fake-graph-access-token",
				"expires_in":   3600,
			})
		case "/v1.0/me/mailFolders/inbox/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":               "m-101",
						"subject":          "OpenAI 验证码",
						"from":             map[string]any{"emailAddress": map[string]any{"name": "OpenAI", "address": "noreply@tm.openai.com"}},
						"receivedDateTime": "2026-11-01T10:00:00Z",
						"isRead":           false,
						"bodyPreview":      "您的 OpenAI 验证码是 584920，请勿分享给他人。",
					},
				},
			})
		case "/v1.0/me/messages/m-101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":               "m-101",
				"subject":          "OpenAI 验证码",
				"from":             map[string]any{"emailAddress": map[string]any{"name": "OpenAI", "address": "noreply@tm.openai.com"}},
				"receivedDateTime": "2026-11-01T10:00:00Z",
				"isRead":           true,
				"bodyPreview":      "您的 OpenAI 验证码是 584920",
				"body": map[string]any{
					"contentType": "html",
					"content":     "<div>您的 OpenAI 验证码是 <strong>584920</strong></div>",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeMS.Close()

	e := newEnv(t)
	oc := outlook.NewClient()
	oc.TokenURLGraph = fakeMS.URL + "/token/graph"
	oc.GraphBaseURL = fakeMS.URL + "/v1.0"
	e.srv.SetOutlookClient(oc)

	admin := e.user("admin@example.com", "correct horse", true)
	alice := e.user("alice@example.com", "correct horse", false)
	bob := e.user("bob@example.com", "correct horse", false)

	bAdmin := e.browser()
	login(e, bAdmin, admin.Email, "correct horse")
	bAlice := e.browser()
	login(e, bAlice, alice.Email, "correct horse")
	bBob := e.browser()
	login(e, bBob, bob.Email, "correct horse")

	// 1. Regular user cannot call admin Outlook endpoints (cannot add or manage accounts)
	r := e.api(bAlice, http.MethodPost, "/api/admin/outlook/accounts/import", map[string]any{
		"accountsText": "a@outlook.com----pwd----cid----rt",
	})
	if r.status != http.StatusForbidden {
		t.Fatalf("expected 403 when regular user imports accounts, got %d %s", r.status, r.body)
	}

	// 2. Admin creates a group and batch imports 2 formatted Outlook mailboxes, assigning them to Alice
	r = e.api(bAdmin, http.MethodPost, "/api/admin/outlook/groups", map[string]any{
		"name":        "AI 业务组",
		"description": "团队共享 Outlook",
		"color":       "#5a3fc0",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create group: %d %s", r.status, r.body)
	}
	var grpResp struct {
		Group struct {
			ID int64 `json:"id"`
		} `json:"group"`
	}
	r.json(t, &grpResp)

	importLines := strings.Join([]string{
		"Molly8418@outlook.com----SuperSecretPwd1----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-secret-rt-1",
		"SecondBox@hotmail.com----SuperSecretPwd2----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-secret-rt-2",
	}, "\n")

	r = e.api(bAdmin, http.MethodPost, "/api/admin/outlook/accounts/import", map[string]any{
		"accountsText": importLines,
		"groupId":      grpResp.Group.ID,
		"remark":       "测试批次",
		"userIds":      []string{alice.ID},
	})
	if r.status != http.StatusOK {
		t.Fatalf("batch import: %d %s", r.status, r.body)
	}
	var impResp struct {
		Added int `json:"added"`
	}
	r.json(t, &impResp)
	if impResp.Added != 2 {
		t.Fatalf("expected 2 added accounts, got %d", impResp.Added)
	}

	// 3. Alice sees the 2 assigned Outlook mailboxes, and response never leaks secrets
	r = e.api(bAlice, http.MethodGet, "/api/outlook/mailboxes", nil)
	if r.status != http.StatusOK {
		t.Fatalf("alice list outlook mailboxes: %d %s", r.status, r.body)
	}
	if strings.Contains(string(r.body), "SuperSecretPwd") || strings.Contains(string(r.body), "secret-rt-") || strings.Contains(string(r.body), "dbc8e03a") {
		t.Fatalf("user mailbox list leaked credentials: %s", r.body)
	}
	var aliceBoxes struct {
		Mailboxes []struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"mailboxes"`
	}
	r.json(t, &aliceBoxes)
	if len(aliceBoxes.Mailboxes) != 2 {
		t.Fatalf("expected 2 mailboxes for Alice, got %+v", aliceBoxes.Mailboxes)
	}

	// 4. Bob (not assigned) sees 0 mailboxes and gets 404 when probing Alice's mailbox
	r = e.api(bBob, http.MethodGet, "/api/outlook/mailboxes", nil)
	if r.status != http.StatusOK {
		t.Fatalf("bob list outlook mailboxes: %d %s", r.status, r.body)
	}
	var bobBoxes struct {
		Mailboxes []any `json:"mailboxes"`
	}
	r.json(t, &bobBoxes)
	if len(bobBoxes.Mailboxes) != 0 {
		t.Fatalf("expected 0 mailboxes for Bob, got %+v", bobBoxes.Mailboxes)
	}

	r = e.api(bBob, http.MethodGet, "/api/outlook/mailboxes/molly8418@outlook.com/emails", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 when Bob probes Alice's mailbox, got %d %s", r.status, r.body)
	}

	// 5. Alice can read emails, extracted verification code, detail, and sandboxed HTML
	r = e.api(bAlice, http.MethodGet, "/api/outlook/mailboxes/molly8418@outlook.com/emails", nil)
	if r.status != http.StatusOK {
		t.Fatalf("alice list emails: %d %s", r.status, r.body)
	}
	var emailsResp struct {
		Items []struct {
			ID               string  `json:"id"`
			VerificationCode *string `json:"verificationCode"`
		} `json:"items"`
		LatestCode *struct {
			Code string `json:"code"`
		} `json:"latestCode"`
	}
	r.json(t, &emailsResp)
	if len(emailsResp.Items) != 1 || emailsResp.LatestCode == nil || emailsResp.LatestCode.Code != "584920" {
		t.Fatalf("unexpected emails response: %s", r.body)
	}

	msgID := emailsResp.Items[0].ID
	r = e.api(bAlice, http.MethodGet, "/api/outlook/mailboxes/molly8418@outlook.com/emails/"+msgID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("alice get email detail: %d %s", r.status, r.body)
	}

	r = e.api(bAlice, http.MethodGet, "/api/outlook/mailboxes/molly8418@outlook.com/emails/"+msgID+"/html", nil)
	if r.status != http.StatusOK || !strings.Contains(string(r.body), "584920") {
		t.Fatalf("alice get email html: %d %s", r.status, r.body)
	}

	// 6. Admin unassigns Alice from Molly8418@outlook.com -> Alice immediately gets 404
	r = e.api(bAdmin, http.MethodPut, "/api/admin/outlook/accounts/"+aliceBoxes.Mailboxes[0].ID+"/grants", map[string]any{
		"userIds": []string{},
	})
	if r.status != http.StatusOK {
		t.Fatalf("admin unassign: %d %s", r.status, r.body)
	}
	r = e.api(bAlice, http.MethodGet, "/api/outlook/mailboxes/"+aliceBoxes.Mailboxes[0].Address+"/emails", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 after unassigning Alice, got %d %s", r.status, r.body)
	}
}
