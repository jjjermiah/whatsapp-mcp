package main

// Recipient normalization and the error type the REST handlers use to report
// bad recipients with a stable machine-readable code.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// maxPhoneDigits is the E.164 limit. Anything longer is almost certainly two
// numbers run together.
const maxPhoneDigits = 15

// minPhoneDigitsBare: bare digits (no "+" or "00" prefix) need at least this
// many to count as carrying a country code; ten or fewer look like a national
// number, e.g. a NANP number missing its leading 1. The bridge never guesses a
// country code.
const minPhoneDigitsBare = 11

// minPhoneDigitsExplicit: when the caller marked the country code with a
// leading "+" or "00", accept the shortest real E.164 numbers (Denmark and
// Singapore are 10 digits including the country code).
const minPhoneDigitsExplicit = 8

// Stable error codes returned in the "error" field of failed requests.
const (
	errCodeInvalidRecipient = "invalid_recipient"
	errCodeNotOnWhatsApp    = "not_on_whatsapp"
)

// recipientError is a caller-fixable problem with a recipient. Status is the
// HTTP status the REST layer should answer with.
type recipientError struct {
	Status  int
	Code    string
	Message string
}

func (e *recipientError) Error() string { return e.Message }

func invalidRecipient(msg string) *recipientError {
	return &recipientError{Status: http.StatusBadRequest, Code: errCodeInvalidRecipient, Message: msg}
}

// normalizeRecipient turns a human-typed phone number into bare digits
// (country code included, no "+"). Full JIDs (anything containing "@") are
// returned untouched.
//
// Accepted noise: leading apostrophe/quote (CSV/spreadsheet text marker), "+",
// whitespace, dashes, dots and parentheses. Input that holds more than one
// number ("a / b", "a, b") or is too short to carry a country code is rejected.
// A leading "+" or "00" is an explicit country-code marker: such input needs
// only 8-15 digits (the "00" is dropped). Bare digits need at least 11.
func normalizeRecipient(input string) (string, error) {
	s := strings.TrimSpace(input)
	if strings.Contains(s, "@") {
		return s, nil
	}
	if s == "" {
		return "", invalidRecipient("recipient is empty")
	}
	if strings.ContainsAny(s, "/,;|\n\r\t&") {
		return "", invalidRecipient(fmt.Sprintf("recipient %s looks like more than one number; send to one number at a time", strconv.Quote(input)))
	}

	s = strings.TrimLeft(s, "'\"\u2018\u2019\u201c\u201d")
	var digits strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+', r == ' ', r == '\u00a0', r == '-', r == '.', r == '(', r == ')':
			// formatting only
		default:
			return "", invalidRecipient(fmt.Sprintf("recipient %s contains an invalid character %s; use digits with country code, or a JID", strconv.Quote(input), strconv.Quote(string(r))))
		}
	}

	d := digits.String()
	explicit := strings.HasPrefix(s, "+") || strings.HasPrefix(d, "00")
	if explicit {
		d = strings.TrimPrefix(d, "00")
	}

	n := len(d)
	switch {
	case digits.Len() == 0:
		return "", invalidRecipient(fmt.Sprintf("recipient %s has no digits", strconv.Quote(input)))
	case explicit && n < minPhoneDigitsExplicit:
		return "", invalidRecipient(fmt.Sprintf("recipient %s has only %d digits after the country-code marker; too short for a phone number", strconv.Quote(input), n))
	case !explicit && n < minPhoneDigitsBare:
		return "", invalidRecipient(fmt.Sprintf("recipient %s has no country code (only %d digits); include it, e.g. 16476698464 for a Canadian number, or write it with a leading + or 00", strconv.Quote(input), n))
	case n > maxPhoneDigits:
		return "", invalidRecipient(fmt.Sprintf("recipient %s has %d digits, more than the %d a phone number can have; it may contain more than one number", strconv.Quote(input), n, maxPhoneDigits))
	}
	return d, nil
}

// writeSendError answers a failed request with
// {"success":false,"error":<code>,"message":<text>} and the error's status.
func writeSendError(w http.ResponseWriter, err *recipientError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   err.Code,
		"message": err.Message,
	})
}

// normalizeOrReject normalizes a recipient/chat field for a handler. On
// failure it writes the 400 response and returns ok=false.
func normalizeOrReject(w http.ResponseWriter, value string) (string, bool) {
	normalized, err := normalizeRecipient(value)
	if err != nil {
		writeSendError(w, err.(*recipientError))
		return "", false
	}
	return normalized, true
}
