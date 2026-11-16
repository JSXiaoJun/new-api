package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/big"
	mrand "math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Characters that stay distinguishable once sheared and covered with noise:
// 0, 1, 2, 5, 8, B, G, I, O, Q, S and Z are left out.
const simpleCaptchaCharset = "ACDEFHJKLMNPRTUVWXY34679"

const (
	simpleCaptchaLength      = 4
	simpleCaptchaWidth       = 120
	simpleCaptchaHeight      = 44
	simpleCaptchaFontSize    = 28
	simpleCaptchaMaxInMemory = 20000
	simpleCaptchaRedisPrefix = "simple_captcha:"
)

// SimpleCaptchaTTL is how long an issued captcha stays valid.
var SimpleCaptchaTTL = 5 * time.Minute

// The parsed font is safe to share; a font.Face is not, so each render makes
// its own face.
var simpleCaptchaFont = sync.OnceValues(func() (*opentype.Font, error) {
	return opentype.Parse(gobold.TTF)
})

type simpleCaptchaEntry struct {
	code      string
	expiresAt time.Time
}

var simpleCaptchaMemory = struct {
	sync.Mutex
	items map[string]simpleCaptchaEntry
}{items: make(map[string]simpleCaptchaEntry)}

// NewSimpleCaptcha issues a single-use captcha and returns its id and answer.
func NewSimpleCaptcha() (string, string, error) {
	var code strings.Builder
	charsetSize := big.NewInt(int64(len(simpleCaptchaCharset)))
	for range simpleCaptchaLength {
		n, err := rand.Int(rand.Reader, charsetSize)
		if err != nil {
			return "", "", err
		}
		code.WriteByte(simpleCaptchaCharset[n.Int64()])
	}
	id := common.GetUUID()

	if common.RedisEnabled {
		if err := common.RDB.Set(context.Background(), simpleCaptchaRedisPrefix+id, code.String(), SimpleCaptchaTTL).Err(); err != nil {
			return "", "", err
		}
		return id, code.String(), nil
	}

	now := time.Now()
	simpleCaptchaMemory.Lock()
	defer simpleCaptchaMemory.Unlock()
	if len(simpleCaptchaMemory.items) >= simpleCaptchaMaxInMemory {
		for key, entry := range simpleCaptchaMemory.items {
			if now.After(entry.expiresAt) {
				delete(simpleCaptchaMemory.items, key)
			}
		}
	}
	// Still full: drop arbitrary entries so a flood of requests cannot grow memory.
	for key := range simpleCaptchaMemory.items {
		if len(simpleCaptchaMemory.items) < simpleCaptchaMaxInMemory {
			break
		}
		delete(simpleCaptchaMemory.items, key)
	}
	simpleCaptchaMemory.items[id] = simpleCaptchaEntry{code: code.String(), expiresAt: now.Add(SimpleCaptchaTTL)}
	return id, code.String(), nil
}

// VerifySimpleCaptcha checks an answer case-insensitively. The captcha is
// consumed by the first attempt whether or not the answer is right, so each
// image allows exactly one guess.
func VerifySimpleCaptcha(id string, answer string) bool {
	answer = strings.ToUpper(strings.TrimSpace(answer))
	if id == "" || answer == "" {
		return false
	}

	var expected string
	if common.RedisEnabled {
		code, err := common.RDB.GetDel(context.Background(), simpleCaptchaRedisPrefix+id).Result()
		if err != nil {
			if !errors.Is(err, redis.Nil) {
				common.SysError(common.LogText("simple captcha redis lookup failed: %s", err.Error()))
			}
			return false
		}
		expected = code
	} else {
		simpleCaptchaMemory.Lock()
		entry, ok := simpleCaptchaMemory.items[id]
		delete(simpleCaptchaMemory.items, id)
		simpleCaptchaMemory.Unlock()
		if !ok || time.Now().After(entry.expiresAt) {
			return false
		}
		expected = entry.code
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(answer)) == 1
}

// RenderSimpleCaptchaPNG draws the code as a distorted raster image with
// noise. A raster keeps the answer out of the response markup.
func RenderSimpleCaptchaPNG(code string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, simpleCaptchaWidth, simpleCaptchaHeight))
	background := color.RGBA{R: uint8(225 + mrand.IntN(30)), G: uint8(225 + mrand.IntN(30)), B: uint8(225 + mrand.IntN(30)), A: 255}
	for y := range simpleCaptchaHeight {
		for x := range simpleCaptchaWidth {
			img.SetRGBA(x, y, background)
		}
	}
	for range 120 {
		img.SetRGBA(mrand.IntN(simpleCaptchaWidth), mrand.IntN(simpleCaptchaHeight), randomCaptchaInk(140))
	}

	drawCaptchaNoiseLines(img, 3)

	parsedFont, err := simpleCaptchaFont()
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{Size: simpleCaptchaFontSize, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer face.Close()
	ascent := face.Metrics().Ascent.Ceil()

	slot := simpleCaptchaWidth / max(len(code), 1)
	for i, ch := range code {
		// Draw the glyph into its own mask, then blend it onto the image with
		// a random shear and offset.
		glyph := image.NewAlpha(image.Rect(0, 0, slot+8, simpleCaptchaHeight))
		advance := font.MeasureString(face, string(ch)).Ceil()
		drawer := font.Drawer{Dst: glyph, Src: image.Opaque, Face: face, Dot: fixed.P((slot+8-advance)/2, ascent)}
		drawer.DrawString(string(ch))

		ink := randomCaptchaInk(110)
		shear := (mrand.Float64() - 0.5) * 0.5
		originX := i*slot - 4 + mrand.IntN(5) - 2
		originY := (simpleCaptchaHeight-ascent)/2 - 4 + mrand.IntN(5) - 2
		bounds := glyph.Bounds()
		for gy := range bounds.Dy() {
			offset := int(shear * float64(gy-bounds.Dy()/2))
			for gx := range bounds.Dx() {
				alpha := uint32(glyph.AlphaAt(gx, gy).A)
				x, y := originX+gx+offset, originY+gy
				if alpha == 0 || !image.Pt(x, y).In(img.Rect) {
					continue
				}
				under := img.RGBAAt(x, y)
				img.SetRGBA(x, y, color.RGBA{
					R: uint8((uint32(ink.R)*alpha + uint32(under.R)*(255-alpha)) / 255),
					G: uint8((uint32(ink.G)*alpha + uint32(under.G)*(255-alpha)) / 255),
					B: uint8((uint32(ink.B)*alpha + uint32(under.B)*(255-alpha)) / 255),
					A: 255,
				})
			}
		}
	}

	// One thin line over the glyphs; the rest stay behind them so the text
	// remains easy for people to read.
	drawCaptchaNoiseLines(img, 1)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func randomCaptchaInk(maxChannel int) color.RGBA {
	return color.RGBA{R: uint8(mrand.IntN(maxChannel)), G: uint8(mrand.IntN(maxChannel)), B: uint8(mrand.IntN(maxChannel)), A: 255}
}

func drawCaptchaNoiseLines(img *image.RGBA, count int) {
	for range count {
		y0 := mrand.IntN(simpleCaptchaHeight)
		y1 := mrand.IntN(simpleCaptchaHeight)
		ink := randomCaptchaInk(180)
		last := simpleCaptchaWidth - 1
		for x := range simpleCaptchaWidth {
			img.SetRGBA(x, y0+(y1-y0)*x/last, ink)
		}
	}
}
