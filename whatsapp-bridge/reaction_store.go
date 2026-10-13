package main

import (
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// storeSentReaction records a reaction sent through /api/react the same way
// handleMessage records an inbound one: media_type="reaction", emoji in
// content (empty means removal), reacted-to message ID in filename. WhatsApp
// does not echo our own sends back as events, so without this messages.db
// keeps showing the reacted-to message as the last word in the chat.
func storeSentReaction(client *whatsmeow.Client, messageStore *MessageStore, chatJID types.JID, reactedToID, emoji string, resp whatsmeow.SendResponse) {
	storedChat := resolveLIDChat(client, chatJID, types.EmptyJID, types.EmptyJID, true)
	timestamp := resp.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	if err := messageStore.StoreMessage(
		resp.ID, storedChat.String(), client.Store.ID.ToNonAD().User, emoji,
		timestamp, true,
		"reaction", reactedToID, "", nil, nil, nil, 0, "",
	); err != nil {
		fmt.Printf("Warning: reaction sent but not stored locally: %v\n", err)
	}
}
