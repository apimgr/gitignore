package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
)

var invalidAccentWarning sync.Once

func validAccent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if len(value) != 7 || value[0] != '#' {
		invalidAccentWarning.Do(func() { log.Print("invalid configuration value for web.ui.accent; using built-in theme colors") })
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		invalidAccentWarning.Do(func() { log.Print("invalid configuration value for web.ui.accent; using built-in theme colors") })
	}
	return err == nil
}

func accentCSS(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if !validAccent(value) {
		return "", false
	}
	red, _ := strconv.ParseUint(value[1:3], 16, 8)
	green, _ := strconv.ParseUint(value[3:5], 16, 8)
	blue, _ := strconv.ParseUint(value[5:7], 16, 8)
	dark := contrastAccent(uint8(red), uint8(green), uint8(blue), 40, 42, 54, true)
	light := contrastAccent(uint8(red), uint8(green), uint8(blue), 255, 255, 255, false)
	return fmt.Sprintf(`:root { --color-primary: %s; --color-primary-bg: %s; }
html.theme-light { --color-primary: %s; --color-primary-bg: %s; }
@media (prefers-color-scheme: light) {
  html.theme-auto { --color-primary: %s; --color-primary-bg: %s; }
}`,
		dark.hex, dark.tint(".15"), light.hex, light.tint(".12"), light.hex, light.tint(".12")), true
}

type accentColor struct {
	r, g, b uint8
	hex     string
}

func accentETag(css string) string {
	digest := sha256.Sum256([]byte(css))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func (c accentColor) tint(alpha string) string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.r, c.g, c.b, alpha)
}

func contrastAccent(r, g, b, br, bg, bb uint8, lighten bool) accentColor {
	h, s, l := rgbToHSL(float64(r)/255, float64(g)/255, float64(b)/255)
	background := luminance(float64(br)/255, float64(bg)/255, float64(bb)/255)
	color := accentColor{r: r, g: g, b: b, hex: fmt.Sprintf("#%02x%02x%02x", r, g, b)}
	if contrastRatio(luminance(float64(r)/255, float64(g)/255, float64(b)/255), background) >= 4.5 {
		return color
	}
	for step := 1; step <= 100; step++ {
		adjusted := l + float64(step)/100
		if !lighten {
			adjusted = l - float64(step)/100
		}
		if adjusted < 0 || adjusted > 1 {
			break
		}
		red, green, blue := hslToRGB(h, s, adjusted)
		if contrastRatio(luminance(red, green, blue), background) >= 4.5 {
			color = accentColor{r: uint8(math.Round(red * 255)), g: uint8(math.Round(green * 255)), b: uint8(math.Round(blue * 255))}
			color.hex = fmt.Sprintf("#%02x%02x%02x", color.r, color.g, color.b)
			return color
		}
	}
	return color
}

func contrastRatio(first, second float64) float64 {
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

func luminance(r, g, b float64) float64 {
	linear := func(value float64) float64 {
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func rgbToHSL(r, g, b float64) (float64, float64, float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	delta := max - min
	l := (max + min) / 2
	if delta == 0 {
		return 0, 0, l
	}
	s := delta / (1 - math.Abs(2*l-1))
	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/delta, 6)
	case g:
		h = (b-r)/delta + 2
	default:
		h = (r-g)/delta + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	return r + m, g + m, b + m
}
