package authninfra

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	challengeImageWidth  = 160
	challengeImageHeight = 54
)

var (
	challengeFontOnce sync.Once
	challengeFont     *opentype.Font
	challengeFontErr  error
)

// renderLoginChallenge draws code as a small PNG. The alphabet is chosen by
// the caller; this only places glyphs, a little jitter, and a few noise lines.
func renderLoginChallenge(code string) ([]byte, error) {
	face, err := newChallengeFace()
	if err != nil {
		return nil, err
	}
	defer closeFace(face)

	img := image.NewRGBA(image.Rect(0, 0, challengeImageWidth, challengeImageHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 0xf4, G: 0xf6, B: 0xf8, A: 0xff}), image.Point{}, draw.Src)
	if err := drawChallengeNoise(img, 5); err != nil {
		return nil, err
	}
	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{R: 0x1a, G: 0x2b, B: 0x4a, A: 0xff}),
		Face: face,
	}
	x := 16
	for _, r := range code {
		y, err := challengeOffset(40, 3)
		if err != nil {
			return nil, err
		}
		dx, err := challengeOffset(0, 2)
		if err != nil {
			return nil, err
		}
		drawer.Dot = fixed.P(x+dx, y)
		drawer.DrawString(string(r))
		x += 34
	}
	if err := drawChallengeNoise(img, 2); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newChallengeFace() (font.Face, error) {
	challengeFontOnce.Do(func() {
		challengeFont, challengeFontErr = opentype.Parse(goregular.TTF)
	})
	if challengeFontErr != nil {
		return nil, challengeFontErr
	}
	return opentype.NewFace(challengeFont, &opentype.FaceOptions{
		Size:    28,
		DPI:     72,
		Hinting: font.HintingNone,
	})
}

func closeFace(face font.Face) {
	c, ok := face.(interface{ Close() error })
	if !ok {
		return
	}
	// The picture is already drawn; a face-close failure cannot change it.
	_ = c.Close()
}

func drawChallengeNoise(img *image.RGBA, lines int) error {
	ink := color.RGBA{R: 0x8a, G: 0x9b, B: 0xb4, A: 0xff}
	for range lines {
		x0, err := challengeOffset(challengeImageWidth/2, challengeImageWidth/2)
		if err != nil {
			return err
		}
		y0, err := challengeOffset(challengeImageHeight/2, challengeImageHeight/2)
		if err != nil {
			return err
		}
		x1, err := challengeOffset(challengeImageWidth/2, challengeImageWidth/2)
		if err != nil {
			return err
		}
		y1, err := challengeOffset(challengeImageHeight/2, challengeImageHeight/2)
		if err != nil {
			return err
		}
		drawLine(img, x0, y0, x1, y1, ink)
	}
	dot := color.RGBA{R: 0xb0, G: 0xbc, B: 0xcc, A: 0xff}
	for range 24 {
		x, err := challengeOffset(challengeImageWidth/2, challengeImageWidth/2)
		if err != nil {
			return err
		}
		y, err := challengeOffset(challengeImageHeight/2, challengeImageHeight/2)
		if err != nil {
			return err
		}
		if image.Pt(x, y).In(img.Bounds()) {
			img.Set(x, y, dot)
		}
	}
	return nil
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	steps := abs(x1 - x0)
	if dy := abs(y1 - y0); dy > steps {
		steps = dy
	}
	if steps == 0 {
		if image.Pt(x0, y0).In(img.Bounds()) {
			img.Set(x0, y0, c)
		}
		return
	}
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		if image.Pt(x, y).In(img.Bounds()) {
			img.Set(x, y, c)
		}
	}
}

// challengeOffset returns base plus a value in [-span, span].
func challengeOffset(base, span int) (int, error) {
	if span <= 0 {
		return base, nil
	}
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return base + int(b[0])%(2*span+1) - span, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
