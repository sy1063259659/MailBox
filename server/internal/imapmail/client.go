package imapmail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

const (
	defaultAddress = "outlook.office365.com:993"
	defaultLimit   = 50
	maxLimit       = 200
)

type DialFunc func(ctx context.Context, address string) (*imapclient.Client, error)

type imapSession struct {
	client  *imapclient.Client
	folders FolderMap
}

func (c Client) ListFolders(ctx context.Context, email, accessToken string) (FolderMap, error) {
	sess, err := c.connect(ctx, email, accessToken)
	if err != nil {
		return nil, err
	}
	defer sess.client.Close()
	return sess.folders, nil
}

func (c Client) ListMessages(ctx context.Context, email, accessToken, folder string, limit int, cursor string) (ListResult, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	sess, err := c.connect(ctx, email, accessToken)
	if err != nil {
		return ListResult{}, err
	}
	defer sess.client.Close()

	mailbox, err := resolveFolder(folder, sess.folders)
	if err != nil {
		return ListResult{}, err
	}
	if _, err := sess.client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return ListResult{}, fmt.Errorf("imapmail: select %q: %w", mailbox, err)
	}

	uids, err := searchPageUIDs(sess.client, limit, cursor)
	if err != nil {
		return ListResult{}, err
	}
	if len(uids) == 0 {
		return ListResult{}, nil
	}

	options := &imap.FetchOptions{
		UID:           true,
		Envelope:      true,
		Flags:         true,
		InternalDate:  true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}

	messages, err := sess.client.Fetch(uidSetFromUIDs(uids), options).Collect()
	if err != nil {
		return ListResult{}, fmt.Errorf("imapmail: fetch list: %w", err)
	}

	summaries := make([]MessageSummary, 0, len(messages))
	for _, msg := range messages {
		summaries = append(summaries, summaryFromFetch(msg))
	}
	sort.Slice(summaries, func(i, j int) bool {
		left, _ := strconv.ParseUint(summaries[i].ID, 10, 32)
		right, _ := strconv.ParseUint(summaries[j].ID, 10, 32)
		return left > right
	})

	var nextCursor string
	if len(summaries) == limit {
		last, _ := strconv.ParseUint(summaries[len(summaries)-1].ID, 10, 32)
		if last > 1 {
			nextCursor = strconv.FormatUint(last-1, 10)
		}
	}

	return ListResult{Messages: summaries, NextCursor: nextCursor}, nil
}

func (c Client) GetMessage(ctx context.Context, email, accessToken, folder string, uid string) (MessageDetail, error) {
	parsedUID, err := parseUID(uid)
	if err != nil {
		return MessageDetail{}, err
	}

	sess, err := c.connect(ctx, email, accessToken)
	if err != nil {
		return MessageDetail{}, err
	}
	defer sess.client.Close()

	mailbox, err := resolveFolder(folder, sess.folders)
	if err != nil {
		return MessageDetail{}, err
	}
	if _, err := sess.client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: select %q: %w", mailbox, err)
	}

	section := &imap.FetchItemBodySection{Peek: true}
	messages, err := sess.client.Fetch(imap.UIDSetNum(parsedUID), &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: fetch message: %w", err)
	}
	if len(messages) == 0 {
		return MessageDetail{}, fmt.Errorf("imapmail: message uid %s not found", uid)
	}

	body := messages[0].FindBodySection(section)
	parsed := parseMessageBody(body)
	detail := detailFromFetch(messages[0])
	if parsed.HTML != "" {
		detail.ContentType = "text/html"
		detail.Content = parsed.HTML
	} else {
		detail.ContentType = "text/plain"
		detail.Content = parsed.Text
	}
	return detail, nil
}

func (c Client) connect(ctx context.Context, email, accessToken string) (*imapSession, error) {
	if strings.TrimSpace(email) == "" {
		return nil, errors.New("imapmail: email is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("imapmail: access token is required")
	}

	dial := c.Dial
	if dial == nil {
		dial = defaultDial
	}
	address := c.Address
	if address == "" {
		address = defaultAddress
	}

	client, err := dial(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("imapmail: dial: %w", err)
	}

	if err := client.Authenticate(newXOAUTH2Client(email, accessToken)); err != nil {
		client.Close()
		return nil, fmt.Errorf("imapmail: xoauth2 authenticate: %w", err)
	}

	folders, err := listFolders(client)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &imapSession{client: client, folders: folders}, nil
}

func defaultDial(ctx context.Context, address string) (*imapclient.Client, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	options := &imapclient.Options{
		Dialer: dialer,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}

	type dialResult struct {
		client *imapclient.Client
		err    error
	}
	done := make(chan dialResult, 1)
	go func() {
		client, err := imapclient.DialTLS(address, options)
		done <- dialResult{client: client, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		return result.client, result.err
	}
}
