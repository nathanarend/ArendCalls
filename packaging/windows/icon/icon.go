// Package icon desenha o ícone do app (telefone branco sobre quadrado colorido)
// em qualquer tamanho e cor, e empacota em .ico. Sem arquivos de imagem: a cor
// indica o estado do servidor na bandeja e o mesmo desenho vira o ícone do .exe.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"regexp"
	"strconv"
)

// Cores de estado usadas na bandeja.
var (
	Green = color.RGBA{0x0F, 0x9D, 0x76, 0xFF} // no ar
	Amber = color.RGBA{0xE3, 0x9B, 0x00, 0xFF} // no ar, conta precisa de atenção
	Red   = color.RGBA{0xD9, 0x30, 0x25, 0xFF} // parado / não responde
	Blue  = color.RGBA{0x1A, 0x73, 0xE8, 0xFF} // iniciando
	Gray  = color.RGBA{0x7A, 0x7F, 0x87, 0xFF} // não instalado / desconhecido
)

// Fone do Material Icons "call" (Apache 2.0), caixa 24×24.
const phonePath = "M20.01 15.38c-1.23 0-2.42-.2-3.53-.56-.35-.12-.74-.03-1.01.24l-1.57 1.97" +
	"c-2.83-1.35-5.48-3.9-6.89-6.83l1.95-1.66c.27-.28.35-.67.24-1.02-.37-1.11-.56-2.3-.56-3.53" +
	" 0-.54-.45-.99-.99-.99H4.19C3.65 3 3 3.24 3 3.99 3 13.28 10.73 21 20.01 21" +
	"c.71 0 .99-.63.99-1.18v-3.45c0-.54-.45-.99-.99-.99z"

// Área do desenho dentro do ícone (coordenadas normalizadas).
const glyph0, glyph1 = 0.21, 0.79

var phone = flatten(phonePath)

const ss = 4 // supersampling por eixo (anti-aliasing)

// Render desenha o ícone size×size na cor base c.
func Render(size int, c color.RGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					if !inRoundRect(x, y, 0.03, 0.03, 0.97, 0.97, 0.22) {
						continue
					}
					col := c
					if inside(phone, x, y) {
						col = white
					}
					r += float64(col.R)
					g += float64(col.G)
					b += float64(col.B)
					a += 255
				}
			}
			if a == 0 {
				continue
			}
			// cor média só das amostras cobertas, alfa = cobertura
			cov := a / 255
			img.SetNRGBA(px, py, color.NRGBA{
				uint8(math.Round(r / cov)), uint8(math.Round(g / cov)), uint8(math.Round(b / cov)),
				uint8(math.Round(a / (ss * ss))),
			})
		}
	}
	return img
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

type pt struct{ x, y float64 }

// inside: regra par/ímpar sobre os polígonos achatados.
func inside(polys [][]pt, x, y float64) bool {
	in := false
	for _, poly := range polys {
		for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
			a, b := poly[i], poly[j]
			if (a.y > y) != (b.y > y) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
				in = !in
			}
		}
	}
	return in
}

var pathTok = regexp.MustCompile(`[MmLlHhVvCcZz]|-?(?:\d+(?:\.\d*)?|\.\d+)`)

// flatten converte um path SVG (M L H V C Z, absolutos e relativos) em
// polígonos, já mapeados da caixa 24×24 para a área do desenho.
func flatten(d string) [][]pt {
	toks := pathTok.FindAllString(d, -1)
	var (
		polys [][]pt
		cur   []pt
		p     pt
		cmd   byte
	)
	num := func(i *int) float64 {
		v, _ := strconv.ParseFloat(toks[*i], 64)
		*i++
		return v
	}
	for i := 0; i < len(toks); {
		if isCmd(toks[i]) {
			cmd = toks[i][0]
			i++
		}
		rel := cmd >= 'a'
		base := pt{}
		if rel {
			base = p
		}
		switch cmd | 0x20 { // minúscula
		case 'm':
			if len(cur) > 0 {
				polys = append(polys, cur)
			}
			p = pt{base.x + num(&i), base.y + num(&i)}
			cur = []pt{p}
			if rel {
				cmd = 'l' // pares seguintes de um M são L
			} else {
				cmd = 'L'
			}
		case 'l':
			p = pt{base.x + num(&i), base.y + num(&i)}
			cur = append(cur, p)
		case 'h':
			p = pt{base.x + num(&i), p.y}
			cur = append(cur, p)
		case 'v':
			p = pt{p.x, base.y + num(&i)}
			cur = append(cur, p)
		case 'c':
			c1 := pt{base.x + num(&i), base.y + num(&i)}
			c2 := pt{base.x + num(&i), base.y + num(&i)}
			e := pt{base.x + num(&i), base.y + num(&i)}
			const steps = 12
			for s := 1; s <= steps; s++ {
				t := float64(s) / steps
				u := 1 - t
				cur = append(cur, pt{
					u*u*u*p.x + 3*u*u*t*c1.x + 3*u*t*t*c2.x + t*t*t*e.x,
					u*u*u*p.y + 3*u*u*t*c1.y + 3*u*t*t*c2.y + t*t*t*e.y,
				})
			}
			p = e
		case 'z':
			if len(cur) > 0 {
				polys = append(polys, cur)
				p = cur[0]
				cur = nil
			}
		}
	}
	if len(cur) > 0 {
		polys = append(polys, cur)
	}
	// Caixa útil do fone: 3..21 nos dois eixos.
	for _, poly := range polys {
		for k := range poly {
			poly[k].x = glyph0 + (poly[k].x-3)/18*(glyph1-glyph0)
			poly[k].y = glyph0 + (poly[k].y-3)/18*(glyph1-glyph0)
		}
	}
	return polys
}

func isCmd(t string) bool {
	switch t[0] {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'Z', 'z':
		return true
	}
	return false
}

// ICO empacota as imagens num .ico. Até 128px vai como DIB 32bpp (o formato
// mais compatível com LoadImage); 256px vai como PNG, como o Windows espera.
func ICO(imgs ...*image.NRGBA) []byte {
	var hdr, body bytes.Buffer
	binary.Write(&hdr, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for _, img := range imgs {
		size := img.Bounds().Dx()
		var data []byte
		if size >= 256 {
			var p bytes.Buffer
			png.Encode(&p, img)
			data = p.Bytes()
		} else {
			data = dib(img)
		}
		dim := uint8(size)
		if size >= 256 {
			dim = 0
		}
		hdr.Write([]byte{dim, dim, 0, 0})
		binary.Write(&hdr, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&hdr, binary.LittleEndian, []uint32{uint32(len(data)), uint32(offset)})
		offset += len(data)
		body.Write(data)
	}
	return append(hdr.Bytes(), body.Bytes()...)
}

func dib(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var b bytes.Buffer
	// BITMAPINFOHEADER: altura dobrada (cor + máscara AND)
	binary.Write(&b, binary.LittleEndian, struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{40, int32(w), int32(h * 2), 1, 32, 0, 0, 0, 0, 0, 0})
	for y := h - 1; y >= 0; y-- { // linhas de baixo para cima
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	stride := ((w + 31) / 32) * 4 // máscara AND zerada (alfa manda)
	b.Write(make([]byte, stride*h))
	return b.Bytes()
}
