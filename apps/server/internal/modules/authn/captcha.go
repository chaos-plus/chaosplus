package authn

import (
	"errors"
	"sync"
	"time"

	"github.com/mojocn/base64Captcha"
)

// captchaDrivers: 每种请求随机选一种验证方式——数字 / 字母 / 数学算式。
// 共享 DefaultMemStore,任一实例可校验(单实例部署)。
var captchaDrivers = []base64Captcha.Driver{
	base64Captcha.NewDriverDigit(64, 220, 4, 0.7, 80),
	base64Captcha.NewDriverString(64, 220, 4, 1, 4, base64Captcha.TxtAlphabet, nil, nil, nil),
	base64Captcha.NewDriverMath(64, 220, 4, 1, nil, nil, nil),
}

var imageCaptcha = base64Captcha.NewCaptcha(captchaDrivers[0], base64Captcha.DefaultMemStore)

// ErrVerificationCodeThrottled: 验证码发送频次超限(60s 内一次)。
var ErrVerificationCodeThrottled = errors.New("verification code send throttled")

var codeThrottle = struct {
	sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

// codeSendThrottled 记录发送时间并返回是否被限流;同一邮箱 60s 内只能发一次。
func codeSendThrottled(email string) bool {
	codeThrottle.Lock()
	defer codeThrottle.Unlock()
	if t, ok := codeThrottle.last[email]; ok && time.Since(t) < 60*time.Second {
		return true
	}
	codeThrottle.last[email] = time.Now()
	return false
}
