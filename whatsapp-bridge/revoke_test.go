package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

type sentRevoke struct {
	to  types.JID
	msg *waProto.Message
}

func recordingSend(sent *[]sentRevoke, err error) revokeSendFunc {
	return func(_ context.Context, to types.JID, msg *waProto.Message) (whatsmeow.SendResponse, error) {
		*sent = append(*sent, sentRevoke{to: to, msg: msg})
		return whatsmeow.SendResponse{}, err
	}
}

func seedRevokeMessage(t *testing.T, ms *MessageStore, chatJID, id string, isFromMe bool) {
	t.Helper()
	if _, err := ms.db.Exec(
		`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, chatJID, "me", "hello", time.Unix(1710000000, 0), isFromMe,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func postRevoke(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/revoke", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestRevokeHandler_RevokesOwnGroupMessageAndMarksDeleted(t *testing.T) {
	client := newTestClient(&mockLIDStore{})
	ms := newTestMessageStore(t)
	group := "120363012345678901@g.us"
	seedRevokeMessage(t, ms, group, "3AMINE", true)

	var sent []sentRevoke
	rec := postRevoke(revokeHandler(client, ms, recordingSend(&sent, nil)),
		`{"chat_jid":"`+group+`","message_id":"3AMINE"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(sent))
	}
	if sent[0].to.String() != group {
		t.Errorf("sent to %s, want %s", sent[0].to, group)
	}
	proto := sent[0].msg.GetProtocolMessage()
	if proto.GetType() != waProto.ProtocolMessage_REVOKE {
		t.Fatalf("type = %v, want REVOKE", proto.GetType())
	}
	if key := proto.GetKey(); key.GetID() != "3AMINE" || !key.GetFromMe() || key.GetRemoteJID() != group {
		t.Errorf("unexpected revoke key: %v", key)
	}
	if _, valid := readDeletedAt(t, ms, group, "3AMINE"); !valid {
		t.Errorf("expected local row to be marked deleted")
	}
}

func TestRevokeHandler_DMUsesLIDAddressingButStoredPhoneChat(t *testing.T) {
	lid := types.JID{User: "99887766", Server: types.HiddenUserServer}
	client := newTestClient(&mockLIDStore{lidByPN: map[types.JID]types.JID{phonePN: lid}})
	ms := newTestMessageStore(t)
	seedRevokeMessage(t, ms, phonePN.String(), "3AMINE", true)

	var sent []sentRevoke
	rec := postRevoke(revokeHandler(client, ms, recordingSend(&sent, nil)),
		`{"chat_jid":"`+phonePN.String()+`","message_id":"3AMINE"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if len(sent) != 1 || sent[0].to != lid {
		t.Fatalf("expected one revoke sent to %s, got %+v", lid, sent)
	}
	if _, valid := readDeletedAt(t, ms, phonePN.String(), "3AMINE"); !valid {
		t.Errorf("expected phone-keyed row to be marked deleted")
	}
}

func TestRevokeHandler_RejectsAndNeverSends(t *testing.T) {
	group := "120363012345678901@g.us"
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"not found", `{"chat_jid":"` + group + `","message_id":"3AMISSING"}`, http.StatusNotFound},
		{"not from me", `{"chat_jid":"` + group + `","message_id":"3ATHEIRS"}`, http.StatusForbidden},
		{"missing message_id", `{"chat_jid":"` + group + `"}`, http.StatusBadRequest},
		{"missing chat_jid", `{"message_id":"3AMINE"}`, http.StatusBadRequest},
		{"bad json", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(&mockLIDStore{})
			ms := newTestMessageStore(t)
			seedRevokeMessage(t, ms, group, "3ATHEIRS", false)

			var sent []sentRevoke
			rec := postRevoke(revokeHandler(client, ms, recordingSend(&sent, nil)), tc.body)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.status, rec.Body.String())
			}
			if len(sent) != 0 {
				t.Fatalf("expected no send, got %d", len(sent))
			}
			if _, valid := readDeletedAt(t, ms, group, "3ATHEIRS"); valid {
				t.Errorf("rejected request must not mark anything deleted")
			}
		})
	}
}

func TestRevokeHandler_SendFailureLeavesRowUndeleted(t *testing.T) {
	client := newTestClient(&mockLIDStore{})
	ms := newTestMessageStore(t)
	group := "120363012345678901@g.us"
	seedRevokeMessage(t, ms, group, "3AMINE", true)

	var sent []sentRevoke
	rec := postRevoke(revokeHandler(client, ms, recordingSend(&sent, errors.New("boom"))),
		`{"chat_jid":"`+group+`","message_id":"3AMINE"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if _, valid := readDeletedAt(t, ms, group, "3AMINE"); valid {
		t.Errorf("failed send must not mark the row deleted")
	}
}

func TestRevokeHandler_RejectsNonPost(t *testing.T) {
	client := newTestClient(&mockLIDStore{})
	var sent []sentRevoke
	h := revokeHandler(client, newTestMessageStore(t), recordingSend(&sent, nil))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/revoke", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
