package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// errorMessages are shown directly in the web UI.
var errorMessages = map[string]string{
	"invalid_json":           "请求格式不正确。",
	"unsupported_media_type": "请求必须使用 JSON。",
	"forbidden_origin":       "请求来源不受信任，请从 XAuth 页面操作。",
	"unauthenticated":        "请先登录。",
	"forbidden":              "需要管理员权限。",
	"not_found":              "找不到该内容。",
	"method_not_allowed":     "不支持该操作。",
	"invalid_credentials":    "邮箱或密码不正确。",
	"account_disabled":       "此账户已停用，请联系管理员。",
	"rate_limited":           "尝试次数过多，请 15 分钟后再试。",
	"signup_disabled":        "当前不开放注册，请联系管理员创建账户。",
	"invalid_email":          "请输入有效的邮箱地址。",
	"invalid_name":           "名称最多 64 个字符，且不能包含控制字符。",
	"password_too_short":     "密码至少需要 8 个字符。",
	"password_too_long":      "密码不能超过 72 字节。",
	"email_taken":            "该邮箱已注册。",
	"wrong_password":         "当前密码不正确。",
	"flow_expired":           "登录请求已过期或已完成，请回到应用重新登录。",
	"login_required":         "需要重新输入密码。",
	"cannot_change_self":     "不能停用自己、取消自己的管理员权限，或强制自己退出。",
	"access_denied":          "你没有使用这个应用的权限。",
	"invalid_access":         "应用的访问范围无效。",
	"unknown_user":           "允许名单里有不存在的用户。",
	"invalid_group":          "没有这个记录分组。",
	"last_key":               "至少要保留一把可用的签名密钥。",
	"last_admin":             "至少需要保留一位启用中的管理员。",
	"invalid_app_name":       "应用名称需要 1 到 64 个字符。",
	"invalid_app_type":       "应用类型无效。",
	"invalid_redirect_uri":   "回调地址无效。",
	"not_confidential":       "只有 Web 应用有客户端密钥。",
	"xmail_not_configured":   "尚未连接 xmail。",
	"xmail_unreachable":      "无法连接 xmail，请检查 XMAIL_BASE_URL 和网络。",
	"xmail_unauthorized":     "xmail 拒绝了 API Key，请检查 XMAIL_API_KEY 是否有效。",
	"xmail_error":            "xmail 返回了错误。",
	"mail_unavailable":       "邮件服务暂时不可用，请稍后再试。",
	"mailbox_disabled":       "这个邮箱已被管理员停用。",
	"mailbox_expired":        "这个邮箱已过期。",
	"mailbox_not_found":      "xmail 里没有这个邮箱，或它在回收站里。",
	"invalid_mailbox":        "邮箱地址无效。",
	"invalid_query":          "查询参数无效。",
	"body_too_large":         "请求内容过大。",
	"invalid_totp":           "验证器密钥无效。请粘贴 Base32 密钥或 otpauth://totp 链接。",
	"totp_shared":            "共享验证器只能由管理员修改。",
	"mail_not_configured":    "这个站点没有配置发信服务器，请联系管理员处理。",
	"invalid_link":           "链接无效。",
	"link_used":              "这个链接已经用过了。",
	"link_expired":           "这个链接已过期或不存在，请重新获取。",
	"passkey_failed":         "通行密钥验证没有通过，请重试或改用密码。",
	"passkey_expired":        "这次验证已过期，请重新开始。",
	"last_login_method":      "这是你最后的登录方式，不能删除。请先设置密码。",
	"internal":               "服务器出错，请稍后再试。",
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeErrorMessage(w, status, code, errorMessages[code])
}

func writeErrorMessage(w http.ResponseWriter, status int, code, msg string) {
	if msg == "" {
		msg = errorMessages["internal"]
	}
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

// decode reads a JSON body of at most 64 KiB into v.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}
