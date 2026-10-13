package main

import (
	"database/sql"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type storedReaction struct {
	sender, content, reactedTo string
	isFromMe                   bool
	timestamp                  time.Time
}

func readStoredReaction(t *testing.T, ms *MessageStore, chatJID, id string) (storedReaction, bool) {
	t.Helper()
	var r storedReaction
	err := ms.db.QueryRow(
		`SELECT sender, content, filename, is_from_me, timestamp FROM messages
		 WHERE id = ? AND chat_jid = ? AND media_type = 'reaction'`,
		id, chatJID,
	).Scan(&r.sender, &r.content, &r.reactedTo, &r.isFromMe, &r.timestamp)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		t.Fatalf("read reaction: %v", err)
	}
	return r, true
}

func TestStoreSentReaction_StoredLikeInboundReaction(t *testing.T) {
	self := types.JID{User: "15550001111", Server: types.DefaultUserServer}
	client := newTestClientWithSelf(&mockLIDStore{}, self)
	ms := newTestMessageStore(t)
	sentAt := time.Unix(1710000100, 0)

	storeSentReaction(client, ms, phonePN, "3ATARGET", "❤️",
		whatsmeow.SendResponse{ID: "3AREACT1", Timestamp: sentAt})

	got, found := readStoredReaction(t, ms, phonePN.String(), "3AREACT1")
	if !found {
		t.Fatal("expected sent reaction to be stored")
	}
	if !got.isFromMe || got.sender != self.User || got.content != "❤️" || got.reactedTo != "3ATARGET" {
		t.Errorf("unexpected stored reaction: %+v", got)
	}
	if !got.timestamp.Equal(sentAt) {
		t.Errorf("timestamp = %v, want %v", got.timestamp, sentAt)
	}
}

func TestStoreSentReaction_RemovalStoredWithEmptyContent(t *testing.T) {
	client := newTestClientWithSelf(&mockLIDStore{}, types.JID{User: "15550001111", Server: types.DefaultUserServer})
	ms := newTestMessageStore(t)

	storeSentReaction(client, ms, phonePN, "3ATARGET", "",
		whatsmeow.SendResponse{ID: "3AREACT2", Timestamp: time.Unix(1710000200, 0)})

	got, found := readStoredReaction(t, ms, phonePN.String(), "3AREACT2")
	if !found {
		t.Fatal("expected reaction removal to be stored, not dropped")
	}
	if got.content != "" || got.reactedTo != "3ATARGET" {
		t.Errorf("unexpected removal row: %+v", got)
	}
}

func TestStoreSentReaction_LIDChatStoredUnderPhoneJID(t *testing.T) {
	lid := types.JID{User: "99887766", Server: types.HiddenUserServer}
	client := newTestClientWithSelf(
		&mockLIDStore{pnByLID: map[types.JID]types.JID{lid: phonePN}},
		types.JID{User: "15550001111", Server: types.DefaultUserServer},
	)
	ms := newTestMessageStore(t)

	storeSentReaction(client, ms, lid, "3ATARGET", "👍",
		whatsmeow.SendResponse{ID: "3AREACT3", Timestamp: time.Unix(1710000300, 0)})

	if _, found := readStoredReaction(t, ms, phonePN.String(), "3AREACT3"); !found {
		t.Fatal("expected reaction under the phone-keyed chat, matching handleMessage")
	}
}
