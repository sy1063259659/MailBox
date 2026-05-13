package imapmail

import (
	"fmt"
	"html"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

var htmlTagRE = regexp.MustCompile(`(?s)<[^>]*>`)

func searchPageUIDs(client *imapclient.Client, limit int, cursor string) ([]imap.UID, error) {
	var criteria imap.SearchCriteria
	if strings.TrimSpace(cursor) != "" {
		cursorUID, err := parseUID(cursor)
		if err != nil {
			return nil, err
		}
		if cursorUID == 0 {
			return nil, nil
		}
		criteria.UID = []imap.UIDSet{uidRange(1, cursorUID)}
	}

	data, err := client.UIDSearch(&criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imapmail: uid search: %w", err)
	}

	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	sortUIDsDesc(uids)
	if len(uids) > limit {
		uids = uids[:limit]
	}
	return uids, nil
}

func summaryFromFetch(msg *imapclient.FetchMessageBuffer) MessageSummary {
	summary := MessageSummary{
		ID:             strconv.FormatUint(uint64(msg.UID), 10),
		IsRead:         hasFlag(msg.Flags, imap.FlagSeen),
		HasAttachments: hasAttachments(msg.BodyStructure),
	}
	if !msg.InternalDate.IsZero() {
		summary.ReceivedAt = msg.InternalDate
	}
	if env := msg.Envelope; env != nil {
		summary.Subject = env.Subject
		summary.From = addressesFromIMAP(env.From)
		summary.To = addressesFromIMAP(env.To)
		summary.Cc = addressesFromIMAP(env.Cc)
		if summary.ReceivedAt.IsZero() {
			summary.ReceivedAt = env.Date
		}
	}

	return summary
}

func detailFromFetch(msg *imapclient.FetchMessageBuffer) MessageDetail {
	detail := MessageDetail{
		ID:     strconv.FormatUint(uint64(msg.UID), 10),
		IsRead: hasFlag(msg.Flags, imap.FlagSeen),
	}
	if !msg.InternalDate.IsZero() {
		detail.ReceivedAt = msg.InternalDate
	}
	if env := msg.Envelope; env != nil {
		detail.Subject = env.Subject
		detail.From = addressesFromIMAP(env.From)
		detail.To = addressesFromIMAP(env.To)
		detail.Cc = addressesFromIMAP(env.Cc)
		if detail.ReceivedAt.IsZero() {
			detail.ReceivedAt = env.Date
		}
	}
	return detail
}

func addressesFromIMAP(addrs []imap.Address) []Address {
	out := make([]Address, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IsGroupStart() || addr.IsGroupEnd() {
			continue
		}
		email := addr.Addr()
		if email == "" && addr.Mailbox != "" {
			email = addr.Mailbox
		}
		out = append(out, Address{Name: addr.Name, Email: email})
	}
	return out
}

func hasFlag(flags []imap.Flag, target imap.Flag) bool {
	for _, flag := range flags {
		if flag == target {
			return true
		}
	}
	return false
}

func hasAttachments(body imap.BodyStructure) bool {
	if body == nil {
		return false
	}
	found := false
	body.Walk(func(_ []int, part imap.BodyStructure) bool {
		disposition := part.Disposition()
		if disposition != nil && strings.EqualFold(disposition.Value, "attachment") {
			found = true
			return false
		}
		if single, ok := part.(*imap.BodyStructureSinglePart); ok && single.Filename() != "" {
			found = true
			return false
		}
		return true
	})
	return found
}

func uidSetFromUIDs(uids []imap.UID) imap.UIDSet {
	var set imap.UIDSet
	set.AddNum(uids...)
	return set
}

func uidRange(start, stop imap.UID) imap.UIDSet {
	var set imap.UIDSet
	set.AddRange(start, stop)
	return set
}

func sortUIDsDesc(uids []imap.UID) {
	for i := 1; i < len(uids); i++ {
		for j := i; j > 0 && uids[j] > uids[j-1]; j-- {
			uids[j], uids[j-1] = uids[j-1], uids[j]
		}
	}
}

func parseUID(uid string) (imap.UID, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(uid), 10, 32)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("imapmail: invalid uid %q", uid)
	}
	return imap.UID(value), nil
}

func previewText(value string) string {
	value = stripHTML(decodeQuotedPrintable(value))
	value = strings.Join(strings.Fields(value), " ")
	const maxRunes = 180
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func stripHTML(value string) string {
	return html.UnescapeString(htmlTagRE.ReplaceAllString(value, " "))
}

func decodeQuotedPrintable(value string) string {
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(value)))
	if err != nil {
		return value
	}
	return string(decoded)
}
