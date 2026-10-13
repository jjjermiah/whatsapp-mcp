package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

// RevokeRequest is the request body for the /api/revoke endpoint.
type RevokeRequest struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
}

// revokeSendFunc sends a built message to a chat. It is satisfied by
// client.SendMessage and replaced in tests so no WhatsApp traffic is needed.
type revokeSendFunc func(ctx context.Context, to types.JID, msg *waProto.Message) (whatsmeow.SendResponse, error)

// registerRevokeEndpoint wires POST /api/revoke ("delete for everyone") onto
// an existing mux. Only messages this account sent can be revoked; admin
// revokes of other people's group messages are intentionally not supported.
func registerRevokeEndpoint(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc, client *whatsmeow.Client, messageStore *MessageStore) {
	send := func(ctx context.Context, to types.JID, msg *waProto.Message) (whatsmeow.SendResponse, error) {
		return client.SendMessage(ctx, to, msg)
	}
	mux.HandleFunc("/api/revoke", auth(revokeHandler(client, messageStore, send)))
}

func revokeHandler(client *whatsmeow.Client, messageStore *MessageStore, send revokeSendFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req RevokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatJID == "" || req.MessageID == "" {
			http.Error(w, "chat_jid and message_id are required", http.StatusBadRequest)
			return
		}
		chatJID, err := types.ParseJID(req.ChatJID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid chat_jid: %v", err), http.StatusBadRequest)
			return
		}

		// messages.db keys DMs by phone JID even when WhatsApp addresses the
		// chat by LID, so look the message up under the stored form.
		storedChat := resolveLIDChat(client, chatJID, types.EmptyJID, types.EmptyJID, true)
		isFromMe, err := messageStore.GetMessageIsFromMe(req.MessageID, storedChat.String())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to look up message: %v", err), http.StatusInternalServerError)
			return
		}
		if isFromMe == nil {
			http.Error(w, "message not found in local store", http.StatusNotFound)
			return
		}
		if !*isFromMe {
			http.Error(w, "only messages sent by this account can be deleted", http.StatusForbidden)
			return
		}

		target := storedChat
		if target.Server == types.DefaultUserServer {
			// Same PN -> LID addressing /api/send used for the original message.
			if target, err = resolveRecipientJID(client, storedChat.String()); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		// An empty sender revokes our own message.
		if _, err := send(context.Background(), target, client.BuildRevoke(target, types.EmptyJID, req.MessageID)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := messageStore.MarkMessageDeleted(req.MessageID, storedChat.String(), time.Now()); err != nil {
			fmt.Printf("Warning: revoked %s but failed to mark it deleted locally: %v\n", req.MessageID, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}
