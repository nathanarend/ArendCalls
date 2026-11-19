// genicon grava o ícone do executável (multi-tamanho) usado pelo build, ou um
// PNG de prévia:
//
//	go run ./cmd/genicon <saida.ico>
//	go run ./cmd/genicon <saida.png>   (256px, para conferir o desenho)
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"

	"arendcalls/launcher/icon"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: genicon <saida.ico|saida.png>")
		os.Exit(2)
	}
	out := os.Args[1]
	var err error
	if strings.HasSuffix(strings.ToLower(out), ".png") {
		var f *os.File
		if f, err = os.Create(out); err == nil {
			err = png.Encode(f, icon.Render(256, icon.Green))
			f.Close()
		}
	} else {
		var imgs []*image.NRGBA
		for _, s := range []int{16, 24, 32, 48, 64, 128, 256} {
			imgs = append(imgs, icon.Render(s, icon.Green))
		}
		err = os.WriteFile(out, icon.ICO(imgs...), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
