package outlook

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Supported mail retrieval methods.
const (
	MethodAuto    = "auto"
	MethodGraph   = "graph"
	MethodIMAPNew = "imap_new"
	MethodIMAPOld = "imap_old"
)

// Default Microsoft OAuth2 and mail server endpoints.
const (
	DefaultTokenURLGraph = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	DefaultTokenURLIMAP  = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	DefaultTokenURLLive  = "https://login.live.com/oauth20_token.srf"
	DefaultGraphBaseURL  = "https://graph.microsoft.com/v1.0"
	DefaultIMAPServerNew = "outlook.live.com:993"
	DefaultIMAPServerOld = "outlook.office365.com:993"
)

var (
	// ErrAccountAbuse indicates Microsoft has locked/suspended the account.
	ErrAccountAbuse = errors.New("账号已被微软风控或封禁 (service abuse mode)")
	// ErrInvalidGrant indicates the refresh_token or client_id is invalid/expired.
	ErrInvalidGrant = errors.New("OAuth2 刷新令牌无效或已过期")
	// ErrNotFound indicates the requested email message was not found.
	ErrNotFound = errors.New("找不到该邮件")
)

// Credentials holds the OAuth2 credentials of one Outlook mailbox.
type Credentials struct {
	Email        string
	ClientID     string
	RefreshToken string
}

// MessageItem is the summary of an email shown in inbox lists.
type MessageItem struct {
	ID               string  `json:"id"`
	Mailbox          string  `json:"mailbox"`
	From             string  `json:"from"`
	FromName         *string `json:"fromName"`
	To               string  `json:"to"`
	Subject          *string `json:"subject"`
	IsRead           bool    `json:"isRead"`
	IsStarred        bool    `json:"isStarred"`
	HasAttachments   bool    `json:"hasAttachments"`
	CreatedAt        int64   `json:"createdAt"`
	Preview          string  `json:"preview"`
	VerificationCode *string `json:"verificationCode"`
	Method           string  `json:"method,omitempty"`
}

// MessageDetail is the full content of an email.
type MessageDetail struct {
	MessageItem
	Cc      string  `json:"cc,omitempty"`
	Text    *string `json:"text"`
	HTML    *string `json:"html,omitempty"`
	HasHTML bool    `json:"hasHtml"`
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

type cachedMessage struct {
	detail    MessageDetail
	expiresAt time.Time
}

// Client fetches Outlook emails via Graph API and IMAP XOAUTH2.
type Client struct {
	HTTPClient    *http.Client
	TokenURLGraph string
	TokenURLIMAP  string
	TokenURLLive  string
	GraphBaseURL  string
	IMAPServerNew string
	IMAPServerOld string
	TLSConfig     *tls.Config

	mu       sync.Mutex
	tokens   map[string]cachedToken
	messages map[string]cachedMessage
}

// NewClient returns a Client configured with Microsoft's production endpoints.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 25 * time.Second,
		},
		TokenURLGraph: DefaultTokenURLGraph,
		TokenURLIMAP:  DefaultTokenURLIMAP,
		TokenURLLive:  DefaultTokenURLLive,
		GraphBaseURL:  DefaultGraphBaseURL,
		IMAPServerNew: DefaultIMAPServerNew,
		IMAPServerOld: DefaultIMAPServerOld,
		tokens:        map[string]cachedToken{},
		messages:      map[string]cachedMessage{},
	}
}

// ValidMethod reports whether m is a supported retrieval method.
func ValidMethod(m string) bool {
	switch m {
	case "", MethodAuto, MethodGraph, MethodIMAPNew, MethodIMAPOld:
		return true
	}
	return false
}

// ListMessages retrieves the most recent inbox messages (up to top) using the
// specified method ("auto", "graph", "imap_new", "imap_old").
func (c *Client) ListMessages(ctx context.Context, creds Credentials, method string, top int) ([]MessageItem, string, error) {
	if top <= 0 {
		top = 20
	}
	if top > 50 {
		top = 50
	}
	creds.Email = NormalizeEmail(creds.Email)

	var methods []string
	switch method {
	case "", MethodAuto:
		methods = []string{MethodGraph, MethodIMAPNew, MethodIMAPOld}
	case MethodGraph, MethodIMAPNew, MethodIMAPOld:
		methods = []string{method}
	default:
		return nil, "", fmt.Errorf("不支持的取信方式：%s", method)
	}

	var lastErr error
	for _, m := range methods {
		var (
			items []MessageItem
			err   error
		)
		switch m {
		case MethodGraph:
			items, err = c.listGraph(ctx, creds, top)
		case MethodIMAPNew:
			items, err = c.listIMAP(ctx, creds, MethodIMAPNew, c.IMAPServerNew, top)
		case MethodIMAPOld:
			items, err = c.listIMAP(ctx, creds, MethodIMAPOld, c.IMAPServerOld, top)
		}
		if err == nil {
			return items, m, nil
		}
		lastErr = err
		if errors.Is(err, ErrAccountAbuse) {
			return nil, m, err
		}
	}
	return nil, "", lastErr
}

// GetMessage retrieves the full detail of a single message.
func (c *Client) GetMessage(ctx context.Context, creds Credentials, messageID, method string) (*MessageDetail, error) {
	creds.Email = NormalizeEmail(creds.Email)
	if cached, ok := c.getCachedMessage(creds.Email, messageID); ok {
		return &cached, nil
	}

	prefix, rawID := splitMessageID(messageID)
	if prefix != "" {
		method = prefix
	}

	var methods []string
	switch method {
	case MethodGraph:
		methods = []string{MethodGraph}
	case MethodIMAPNew:
		methods = []string{MethodIMAPNew}
	case MethodIMAPOld:
		methods = []string{MethodIMAPOld}
	default:
		methods = []string{MethodGraph, MethodIMAPNew, MethodIMAPOld}
	}

	var lastErr error
	for _, m := range methods {
		var (
			detail *MessageDetail
			err    error
		)
		switch m {
		case MethodGraph:
			detail, err = c.getGraphMessage(ctx, creds, rawID)
		case MethodIMAPNew:
			detail, err = c.getIMAPMessage(ctx, creds, MethodIMAPNew, c.IMAPServerNew, rawID)
		case MethodIMAPOld:
			detail, err = c.getIMAPMessage(ctx, creds, MethodIMAPOld, c.IMAPServerOld, rawID)
		}
		if err == nil && detail != nil {
			c.putCachedMessage(creds.Email, detail.ID, *detail)
			if detail.ID != messageID {
				c.putCachedMessage(creds.Email, messageID, *detail)
			}
			return detail, nil
		}
		lastErr = err
		if errors.Is(err, ErrAccountAbuse) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNotFound
}

func splitMessageID(id string) (method, raw string) {
	switch {
	case strings.HasPrefix(id, "g64:"):
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "g64:"))
		if err != nil {
			return MethodGraph, strings.TrimPrefix(id, "g64:")
		}
		return MethodGraph, string(b)
	case strings.HasPrefix(id, "g:"):
		return MethodGraph, strings.TrimPrefix(id, "g:")
	case strings.HasPrefix(id, "in:"):
		return MethodIMAPNew, strings.TrimPrefix(id, "in:")
	case strings.HasPrefix(id, "io:"):
		return MethodIMAPOld, strings.TrimPrefix(id, "io:")
	default:
		return "", id
	}
}

func prefixForMethod(method, rawID string) string {
	switch method {
	case MethodGraph:
		if strings.ContainsAny(rawID, "/+?\\# ") {
			return "g64:" + base64.RawURLEncoding.EncodeToString([]byte(rawID))
		}
		return "g:" + rawID
	case MethodIMAPNew:
		return "in:" + rawID
	case MethodIMAPOld:
		return "io:" + rawID
	default:
		return rawID
	}
}

// ==================== Token Acquisition & Caching ====================

func (c *Client) getAccessToken(ctx context.Context, creds Credentials, method string) (string, error) {
	cacheKey := creds.Email + "|" + creds.ClientID + "|" + creds.RefreshToken + "|" + method
	now := time.Now()

	c.mu.Lock()
	if tok, ok := c.tokens[cacheKey]; ok && now.Before(tok.expiresAt) {
		c.mu.Unlock()
		return tok.token, nil
	}
	c.mu.Unlock()

	form := url.Values{
		"client_id":     {creds.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
	}
	var tokenURL string
	switch method {
	case MethodGraph:
		tokenURL = c.TokenURLGraph
		form.Set("scope", "https://graph.microsoft.com/.default")
	case MethodIMAPNew:
		tokenURL = c.TokenURLIMAP
		form.Set("scope", "https://outlook.office.com/IMAP.AccessAsUser.All offline_access")
	case MethodIMAPOld:
		tokenURL = c.TokenURLLive
	default:
		return "", fmt.Errorf("unknown token method: %s", method)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("请求微软令牌端点失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))

	if resp.StatusCode != http.StatusOK {
		bodyStr := string(body)
		if strings.Contains(bodyStr, "service abuse mode") {
			return "", ErrAccountAbuse
		}
		if strings.Contains(bodyStr, "invalid_grant") || strings.Contains(bodyStr, "AADSTS") {
			return "", ErrInvalidGrant
		}
		return "", fmt.Errorf("获取访问令牌失败 (HTTP %d)", resp.StatusCode)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		return "", fmt.Errorf("解析访问令牌响应失败")
	}

	ttl := 45 * time.Minute
	if parsed.ExpiresIn > 180 {
		ttl = time.Duration(parsed.ExpiresIn-120) * time.Second
	}

	c.mu.Lock()
	if len(c.tokens) > 1000 {
		for k, v := range c.tokens {
			if now.After(v.expiresAt) {
				delete(c.tokens, k)
			}
		}
	}
	c.tokens[cacheKey] = cachedToken{
		token:     parsed.AccessToken,
		expiresAt: now.Add(ttl),
	}
	c.mu.Unlock()

	return parsed.AccessToken, nil
}

func (c *Client) getCachedMessage(mailbox, id string) (MessageDetail, bool) {
	key := mailbox + "|" + id
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if item, ok := c.messages[key]; ok && now.Before(item.expiresAt) {
		return item.detail, true
	}
	return MessageDetail{}, false
}

func (c *Client) putCachedMessage(mailbox, id string, detail MessageDetail) {
	key := mailbox + "|" + id
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) > 500 {
		for k, v := range c.messages {
			if now.After(v.expiresAt) {
				delete(c.messages, k)
			}
		}
	}
	c.messages[key] = cachedMessage{
		detail:    detail,
		expiresAt: now.Add(5 * time.Minute),
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// ==================== Method 1: Microsoft Graph API ====================

type graphRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type graphMessage struct {
	ID               string           `json:"id"`
	Subject          string           `json:"subject"`
	From             graphRecipient   `json:"from"`
	ToRecipients     []graphRecipient `json:"toRecipients"`
	CcRecipients     []graphRecipient `json:"ccRecipients"`
	ReceivedDateTime string           `json:"receivedDateTime"`
	IsRead           bool             `json:"isRead"`
	HasAttachments   bool             `json:"hasAttachments"`
	BodyPreview      string           `json:"bodyPreview"`
	Body             struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

func (c *Client) listGraph(ctx context.Context, creds Credentials, top int) ([]MessageItem, error) {
	token, err := c.getAccessToken(ctx, creds, MethodGraph)
	if err != nil {
		return nil, err
	}

	u := strings.TrimRight(c.GraphBaseURL, "/") + "/me/mailFolders/inbox/messages"
	params := url.Values{
		"$top":     {strconv.Itoa(top)},
		"$select":  {"id,subject,from,toRecipients,receivedDateTime,isRead,hasAttachments,bodyPreview"},
		"$orderby": {"receivedDateTime desc"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "outlook.body-content-type=\"text\"")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Graph API 请求失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Graph API 返回 HTTP %d", resp.StatusCode)
	}

	var data struct {
		Value []graphMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析 Graph API 邮件列表失败：%w", err)
	}

	items := make([]MessageItem, 0, len(data.Value))
	for _, m := range data.Value {
		items = append(items, graphToMessageItem(creds.Email, m))
	}
	return items, nil
}

func (c *Client) getGraphMessage(ctx context.Context, creds Credentials, rawID string) (*MessageDetail, error) {
	token, err := c.getAccessToken(ctx, creds, MethodGraph)
	if err != nil {
		return nil, err
	}

	u := strings.TrimRight(c.GraphBaseURL, "/") + "/me/messages/" + url.PathEscape(rawID)
	params := url.Values{
		"$select": {"id,subject,from,toRecipients,ccRecipients,receivedDateTime,isRead,hasAttachments,body,bodyPreview"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "outlook.body-content-type=\"html\"")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Graph API 获取邮件详情失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Graph API 详情返回 HTTP %d", resp.StatusCode)
	}

	var m graphMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("解析 Graph API 邮件详情失败：%w", err)
	}

	var textBody, htmlBody string
	if strings.EqualFold(m.Body.ContentType, "html") {
		htmlBody = m.Body.Content
		textBody = StripHTML(htmlBody)
		if textBody == "" {
			textBody = m.BodyPreview
		}
	} else {
		textBody = m.Body.Content
		if textBody == "" {
			textBody = m.BodyPreview
		}
	}

	toList := formatGraphRecipients(m.ToRecipients)
	if toList == "" {
		toList = creds.Email
	}
	ccList := formatGraphRecipients(m.CcRecipients)
	createdAt := ParseDate(m.ReceivedDateTime)
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}

	detail := buildMessageDetail(
		prefixForMethod(MethodGraph, m.ID),
		creds.Email,
		MethodGraph,
		strings.TrimSpace(m.Subject),
		strings.TrimSpace(m.From.EmailAddress.Name),
		strings.TrimSpace(m.From.EmailAddress.Address),
		toList,
		ccList,
		createdAt,
		textBody,
		htmlBody,
		m.HasAttachments,
	)
	detail.IsRead = m.IsRead
	if detail.Preview == "" && m.BodyPreview != "" {
		detail.Preview = truncateRunes(strings.Join(strings.Fields(m.BodyPreview), " "), 200)
	}
	return &detail, nil
}

func graphToMessageItem(mailbox string, m graphMessage) MessageItem {
	subj := strings.TrimSpace(m.Subject)
	fromName := strings.TrimSpace(m.From.EmailAddress.Name)
	fromAddr := strings.TrimSpace(m.From.EmailAddress.Address)
	if fromAddr == "" {
		fromAddr = "unknown"
	}
	toList := formatGraphRecipients(m.ToRecipients)
	if toList == "" {
		toList = mailbox
	}
	createdAt := ParseDate(m.ReceivedDateTime)
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	preview := truncateRunes(strings.Join(strings.Fields(m.BodyPreview), " "), 200)
	code := ExtractCode(subj, m.BodyPreview, "")

	var subjPtr, fromNamePtr, codePtr *string
	if subj != "" {
		subjPtr = &subj
	}
	if fromName != "" {
		fromNamePtr = &fromName
	}
	if code != "" {
		codePtr = &code
	}

	return MessageItem{
		ID:               prefixForMethod(MethodGraph, m.ID),
		Mailbox:          mailbox,
		From:             fromAddr,
		FromName:         fromNamePtr,
		To:               toList,
		Subject:          subjPtr,
		IsRead:           m.IsRead,
		IsStarred:        false,
		HasAttachments:   m.HasAttachments,
		CreatedAt:        createdAt,
		Preview:          preview,
		VerificationCode: codePtr,
		Method:           MethodGraph,
	}
}

func formatGraphRecipients(list []graphRecipient) string {
	if len(list) == 0 {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, r := range list {
		addr := strings.TrimSpace(r.EmailAddress.Address)
		if addr != "" {
			out = append(out, addr)
		}
	}
	return strings.Join(out, ", ")
}

// ==================== Method 2 & 3: IMAP XOAUTH2 ====================

var reLiteralSize = regexp.MustCompile(`\{(\d+)\}\r?\n$`)

type imapConn struct {
	conn       net.Conn
	br         *bufio.Reader
	seq        int
	stopCancel func() bool
}

func dialIMAP(ctx context.Context, addr string, tlsCfg *tls.Config) (*imapConn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 IMAP 服务器 %s 失败：%w", addr, err)
	}
	host, _, _ := net.SplitHostPort(addr)
	cfg := &tls.Config{ServerName: host}
	if tlsCfg != nil {
		cfg = tlsCfg.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = host
		}
	}
	tlsConn := tls.Client(rawConn, cfg)
	_ = tlsConn.SetDeadline(time.Now().Add(25 * time.Second))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("IMAP TLS 握手失败：%w", err)
	}

	stopCancel := context.AfterFunc(ctx, func() {
		_ = tlsConn.Close()
	})
	ic := &imapConn{
		conn:       tlsConn,
		br:         bufio.NewReaderSize(tlsConn, 64<<10),
		stopCancel: stopCancel,
	}
	// Read server greeting
	line, err := ic.readLine()
	if err != nil {
		_ = ic.Close()
		return nil, fmt.Errorf("读取 IMAP 欢迎响应失败：%w", err)
	}
	if !strings.HasPrefix(line, "* OK") {
		_ = ic.Close()
		return nil, fmt.Errorf("IMAP 服务器拒绝连接：%s", strings.TrimSpace(line))
	}
	return ic, nil
}

func (ic *imapConn) Close() error {
	if ic.stopCancel != nil {
		ic.stopCancel()
	}
	_ = ic.conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = fmt.Fprintf(ic.conn, "Z99 LOGOUT\r\n")
	return ic.conn.Close()
}

func (ic *imapConn) nextTag() string {
	ic.seq++
	return fmt.Sprintf("A%d", ic.seq)
}

func (ic *imapConn) readLine() (string, error) {
	return ic.br.ReadString('\n')
}

func (ic *imapConn) authenticateXOAUTH2(email, accessToken string) error {
	authRaw := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", email, accessToken)
	authB64 := base64.StdEncoding.EncodeToString([]byte(authRaw))
	tag := ic.nextTag()
	if _, err := fmt.Fprintf(ic.conn, "%s AUTHENTICATE XOAUTH2\r\n", tag); err != nil {
		return err
	}
	sentCreds := false
	for {
		line, err := ic.readLine()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "+") {
			if !sentCreds {
				sentCreds = true
				if _, err := fmt.Fprintf(ic.conn, "%s\r\n", authB64); err != nil {
					return err
				}
			} else {
				// Error challenge continuation from server; send empty line to receive tagged NO/BAD.
				if _, err := fmt.Fprintf(ic.conn, "\r\n"); err != nil {
					return err
				}
			}
			continue
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.HasPrefix(line, tag+" OK") {
				return nil
			}
			return fmt.Errorf("IMAP XOAUTH2 认证失败：%s", strings.TrimSpace(line[len(tag)+1:]))
		}
	}
}

func (ic *imapConn) selectInbox() error {
	tag := ic.nextTag()
	if _, err := fmt.Fprintf(ic.conn, "%s SELECT \"INBOX\"\r\n", tag); err != nil {
		return err
	}
	for {
		line, err := ic.readLine()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.HasPrefix(line, tag+" OK") {
				return nil
			}
			return fmt.Errorf("IMAP SELECT INBOX 失败：%s", strings.TrimSpace(line[len(tag)+1:]))
		}
	}
}

func (ic *imapConn) searchAll() ([]string, error) {
	tag := ic.nextTag()
	if _, err := fmt.Fprintf(ic.conn, "%s SEARCH ALL\r\n", tag); err != nil {
		return nil, err
	}
	var ids []string
	for {
		line, err := ic.readLine()
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* SEARCH") {
			fields := strings.Fields(trimmed)
			if len(fields) > 2 {
				ids = append(ids, fields[2:]...)
			}
			continue
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.HasPrefix(line, tag+" OK") {
				return ids, nil
			}
			return nil, fmt.Errorf("IMAP SEARCH 失败：%s", strings.TrimSpace(line[len(tag)+1:]))
		}
	}
}

func (ic *imapConn) fetchRFC822(seqID string) ([]byte, error) {
	tag := ic.nextTag()
	if _, err := fmt.Fprintf(ic.conn, "%s FETCH %s (BODY.PEEK[])\r\n", tag, seqID); err != nil {
		return nil, err
	}
	var raw []byte
	for {
		line, err := ic.readLine()
		if err != nil {
			return nil, err
		}
		if m := reLiteralSize.FindStringSubmatch(line); len(m) == 2 {
			size, err := strconv.Atoi(m[1])
			if err != nil || size < 0 || size > 16<<20 {
				return nil, fmt.Errorf("无效的 IMAP 邮件大小：%s", m[1])
			}
			buf := make([]byte, size)
			if _, err := io.ReadFull(ic.br, buf); err != nil {
				return nil, err
			}
			raw = buf
			continue
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.HasPrefix(line, tag+" OK") {
				if raw == nil {
					return nil, ErrNotFound
				}
				return raw, nil
			}
			return nil, fmt.Errorf("IMAP FETCH 失败：%s", strings.TrimSpace(line[len(tag)+1:]))
		}
	}
}

func (c *Client) listIMAP(ctx context.Context, creds Credentials, method, serverAddr string, top int) ([]MessageItem, error) {
	token, err := c.getAccessToken(ctx, creds, method)
	if err != nil {
		return nil, err
	}
	ic, err := dialIMAP(ctx, serverAddr, c.TLSConfig)
	if err != nil {
		return nil, err
	}
	defer ic.Close()

	if err := ic.authenticateXOAUTH2(creds.Email, token); err != nil {
		return nil, err
	}
	if err := ic.selectInbox(); err != nil {
		return nil, err
	}
	allIDs, err := ic.searchAll()
	if err != nil {
		return nil, err
	}
	if len(allIDs) == 0 {
		return []MessageItem{}, nil
	}

	start := len(allIDs) - top
	if start < 0 {
		start = 0
	}
	recent := allIDs[start:]

	items := make([]MessageItem, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		seqID := recent[i]
		fullID := prefixForMethod(method, seqID)
		if cached, ok := c.getCachedMessage(creds.Email, fullID); ok {
			items = append(items, cached.MessageItem)
			continue
		}
		raw, err := ic.fetchRFC822(seqID)
		if err != nil {
			continue
		}
		detail := ParseRFC822(fullID, creds.Email, method, raw)
		c.putCachedMessage(creds.Email, fullID, detail)
		items = append(items, detail.MessageItem)
	}
	return items, nil
}

func (c *Client) getIMAPMessage(ctx context.Context, creds Credentials, method, serverAddr, seqID string) (*MessageDetail, error) {
	for _, r := range seqID {
		if r < '0' || r > '9' {
			return nil, ErrNotFound
		}
	}
	fullID := prefixForMethod(method, seqID)
	if cached, ok := c.getCachedMessage(creds.Email, fullID); ok {
		return &cached, nil
	}

	token, err := c.getAccessToken(ctx, creds, method)
	if err != nil {
		return nil, err
	}
	ic, err := dialIMAP(ctx, serverAddr, c.TLSConfig)
	if err != nil {
		return nil, err
	}
	defer ic.Close()

	if err := ic.authenticateXOAUTH2(creds.Email, token); err != nil {
		return nil, err
	}
	if err := ic.selectInbox(); err != nil {
		return nil, err
	}
	raw, err := ic.fetchRFC822(seqID)
	if err != nil {
		return nil, err
	}
	detail := ParseRFC822(fullID, creds.Email, method, raw)
	c.putCachedMessage(creds.Email, fullID, detail)
	return &detail, nil
}
