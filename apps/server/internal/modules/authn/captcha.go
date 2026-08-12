package authn

import "github.com/mojocn/base64Captcha"

// captchaDrivers: 每种请求随机选一种验证方式——数字 / 字母 / 数学算式。
// 共享 DefaultMemStore,任一实例可校验(单实例部署)。
var captchaDrivers = []base64Captcha.Driver{
	base64Captcha.NewDriverDigit(64, 220, 4, 0.7, 80),
	base64Captcha.NewDriverString(64, 220, 4, 1, 4, base64Captcha.TxtAlphabet, nil, nil, nil),
	base64Captcha.NewDriverMath(64, 220, 4, 1, nil, nil, nil),
}

var imageCaptcha = base64Captcha.NewCaptcha(captchaDrivers[0], base64Captcha.DefaultMemStore)
