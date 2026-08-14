package authn

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mojocn/base64Captcha"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
	"github.com/wenlng/go-captcha/v2/slide"
)

type captchaKind string

const (
	captchaText  captchaKind = "text"
	captchaSlide captchaKind = "slide"
	captchaClick captchaKind = "click"
)

const (
	slideW, slideH = 300, 220
	slidePadding   = 8
	clickPadding   = 20
	interactiveTTL = 5 * time.Minute
)

var (
	slideBackgrounds []image.Image
	clickBackgrounds []image.Image
	slideGraph       *slide.GraphImage
)

func secureIntn(bound int) int {
	if bound <= 0 {
		panic("captcha random bound must be positive")
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(bound)))
	if err != nil {
		panic(fmt.Errorf("read captcha entropy: %w", err))
	}
	result, err := strconv.Atoi(value.String())
	if err != nil {
		panic(fmt.Errorf("convert captcha random value: %w", err))
	}
	return result
}

func secureByte(bound byte) byte {
	if bound == 0 {
		panic("captcha random byte bound must be positive")
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(bound)))
	if err != nil {
		panic(fmt.Errorf("read captcha entropy: %w", err))
	}
	raw := value.Bytes()
	if len(raw) == 0 {
		return 0
	}
	return raw[0]
}

func secureByteOffset(base, bound byte) byte {
	if bound == 0 || base > 255-(bound-1) {
		panic("captcha random byte range is invalid")
	}
	return base + secureByte(bound)
}

func init() {
	for i := 0; i < 3; i++ {
		bg := genCaptchaBackground()
		slideBackgrounds = append(slideBackgrounds, bg)
		clickBackgrounds = append(clickBackgrounds, bg)
	}
	slideGraph = genSlideGraph()
}

// interactiveAnswers: 滑块/点选答案的内存存储,短时效,一次性。
var interactiveAnswers = struct {
	sync.Mutex
	val map[string]string
	exp map[string]time.Time
}{val: map[string]string{}, exp: map[string]time.Time{}}

func storeInteractiveAnswer(id, v string) {
	interactiveAnswers.Lock()
	defer interactiveAnswers.Unlock()
	interactiveAnswers.val[id] = v
	interactiveAnswers.exp[id] = time.Now().Add(interactiveTTL)
}

func takeInteractiveAnswer(id string) string {
	interactiveAnswers.Lock()
	defer interactiveAnswers.Unlock()
	v, ok := interactiveAnswers.val[id]
	if !ok {
		return ""
	}
	delete(interactiveAnswers.val, id)
	delete(interactiveAnswers.exp, id)
	return v
}

// genCaptchaBackground: 渐变 + 噪点圆的底图,滑块/点选共用。
func genCaptchaBackground() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, slideW, slideH))
	top := color.RGBA{secureByteOffset(90, 110), secureByteOffset(70, 90), secureByteOffset(120, 100), 255}
	bottom := color.RGBA{secureByteOffset(25, 50), secureByteOffset(35, 55), secureByteOffset(50, 70), 255}
	for y := 0; y < slideH; y++ {
		t := float64(y) / float64(slideH)
		c := color.RGBA{
			uint8(float64(top.R) + (float64(bottom.R)-float64(top.R))*t),
			uint8(float64(top.G) + (float64(bottom.G)-float64(top.G))*t),
			uint8(float64(top.B) + (float64(bottom.B)-float64(top.B))*t),
			255,
		}
		draw.Draw(img, image.Rect(0, y, slideW, y+1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	for i := 0; i < 24; i++ {
		cx, cy, r := secureIntn(slideW), secureIntn(slideH), 6+secureIntn(22)
		c := color.RGBA{secureByte(255), secureByte(255), secureByte(255), secureByteOffset(50, 90)}
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if dx*dx+dy*dy <= r*r {
					x, y := cx+dx, cy+dy
					if x >= 0 && x < slideW && y >= 0 && y < slideH {
						img.SetRGBA(x, y, c)
					}
				}
			}
		}
	}
	return img
}

// genSlideGraph: 50x50 带缺口凸起的拼图块(mask/overlay/shadow 三件套)。
func genSlideGraph() *slide.GraphImage {
	const s = 50
	mask := image.NewRGBA(image.Rect(0, 0, s, s))
	overlay := image.NewRGBA(image.Rect(0, 0, s, s))
	shadow := image.NewRGBA(image.Rect(0, 0, s, s))
	tint := color.RGBA{secureByteOffset(120, 100), secureByteOffset(100, 110), secureByteOffset(130, 110), 255}
	shape := func(x, y int) bool {
		if x >= 3 && x < s-5 && y >= 3 && y < s-3 {
			return true
		}
		if x >= s-5 && x < s-1 && y >= s/2-7 && y < s/2+7 {
			return true
		}
		return false
	}
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if !shape(x, y) {
				continue
			}
			mask.Set(x, y, color.White)
			overlay.Set(x, y, tint)
			shadow.Set(x, y, color.RGBA{A: 150})
		}
	}
	return &slide.GraphImage{OverlayImage: overlay, ShadowImage: shadow, MaskImage: mask}
}

// captchaOutput: 统一验证码响应,按 type 携带对应图片。
type captchaOutput struct {
	Type        string `json:"type"`
	CaptchaID   string `json:"captcha_id"`
	ImageBase64 string `json:"image_base64,omitempty"` // text
	MasterImage string `json:"master_image,omitempty"` // slide/click 主图
	TileImage   string `json:"tile_image,omitempty"`   // slide 拼图块
	ThumbImage  string `json:"thumb_image,omitempty"`  // click 目标字符
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// issueCaptcha 随机返回一种验证方式(text / slide / click)。
func issueCaptcha() (*captchaOutput, error) {
	var kind captchaKind
	switch secureIntn(3) {
	case 0:
		kind = captchaText
	case 1:
		kind = captchaSlide
	default:
		kind = captchaClick
	}
	switch kind {
	case captchaText:
		id, b64, _, err := imageCaptcha.Generate()
		if err != nil {
			return nil, err
		}
		return &captchaOutput{Type: "text", CaptchaID: id, ImageBase64: b64}, nil
	case captchaSlide:
		builder := slide.NewBuilder(slide.WithImageSize(option.Size{Width: slideW, Height: slideH}))
		builder.SetResources(
			slide.WithGraphImages([]*slide.GraphImage{slideGraph}),
			slide.WithBackgrounds(slideBackgrounds),
		)
		capt := builder.Make()
		data, err := capt.Generate()
		if err != nil {
			return nil, err
		}
		block := data.GetData()
		id := mustRandomToken(16)
		storeInteractiveAnswer(id, fmt.Sprintf("S:%d:%d", block.DX, block.DY))
		master, err := data.GetMasterImage().ToBase64()
		if err != nil {
			return nil, err
		}
		tile, err := data.GetTileImage().ToBase64()
		if err != nil {
			return nil, err
		}
		return &captchaOutput{
			Type: "slide", CaptchaID: id,
			MasterImage: master, TileImage: tile,
			Width: slideW, Height: slideH,
		}, nil
	case captchaClick:
		builder := click.NewBuilder()
		builder.SetResources(
			click.WithBackgrounds(clickBackgrounds),
			click.WithChars([]string{
				"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
				"a", "b", "c", "d", "e", "f", "g", "h", "j", "k", "m", "n",
				"p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
			}),
			click.WithFonts(base64Captcha.DefaultEmbeddedFonts.LoadFontsByNames([]string{
				"fonts/3Dumb.ttf", "fonts/Comismsh.ttf", "fonts/ApothecaryFont.ttf",
			})),
		)
		capt := builder.Make()
		data, err := capt.Generate()
		if err != nil {
			return nil, err
		}
		dotsJSON, err := json.Marshal(data.GetData())
		if err != nil {
			return nil, err
		}
		id := mustRandomToken(16)
		storeInteractiveAnswer(id, "C:"+string(dotsJSON))
		master, err := data.GetMasterImage().ToBase64()
		if err != nil {
			return nil, err
		}
		thumb, err := data.GetThumbImage().ToBase64()
		if err != nil {
			return nil, err
		}
		return &captchaOutput{
			Type: "click", CaptchaID: id,
			MasterImage: master, ThumbImage: thumb,
			Width: slideW, Height: slideH,
		}, nil
	}
	return nil, fmt.Errorf("unknown captcha kind")
}

// VerifyCaptcha 按类型校验验证码(一次性)。
func VerifyCaptcha(kind, id, answer string) bool {
	if id == "" || answer == "" {
		return false
	}
	switch captchaKind(kind) {
	case captchaText:
		return imageCaptcha.Verify(id, answer, true)
	case captchaSlide:
		stored := takeInteractiveAnswer(id)
		var tileX, tileY, ux int
		if _, err := fmt.Sscanf(stored, "S:%d:%d", &tileX, &tileY); err != nil {
			return false
		}
		if _, err := fmt.Sscanf(answer, "%d", &ux); err != nil {
			return false
		}
		// 基础滑块:Y 固定在轨道上(tileY),只校验横向位移。
		return slide.Validate(tileX, tileY, ux, tileY, slidePadding)
	case captchaClick:
		stored := takeInteractiveAnswer(id)
		if !strings.HasPrefix(stored, "C:") {
			return false
		}
		var dots map[int]*click.Dot
		if err := json.Unmarshal([]byte(strings.TrimPrefix(stored, "C:")), &dots); err != nil {
			return false
		}
		// answer: "x1,y1;x2,y2" — 每个点击须命中一个不同的目标点。
		used := make(map[int]bool, len(dots))
		for _, part := range strings.Split(answer, ";") {
			var ux, uy int
			if _, err := fmt.Sscanf(part, "%d,%d", &ux, &uy); err != nil {
				return false
			}
			matched := false
			for idx, d := range dots {
				if used[idx] {
					continue
				}
				if click.Validate(ux, uy, d.X, d.Y, slideW, slideH, clickPadding) {
					used[idx] = true
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return len(used) == len(dots)
	}
	return false
}
