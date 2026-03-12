package api

import (
	_ "embed"
	"fmt"

	"github.com/go-pdf/fpdf"
)

const exportPDFFontFamily = "ovumcy-dejavu"

var (
	//go:embed assets/fonts/DejaVuSansCondensed.ttf
	exportPDFRegularFont []byte
	//go:embed assets/fonts/DejaVuSansCondensed-Bold.ttf
	exportPDFBoldFont []byte
)

func configureExportPDFFonts(pdf *fpdf.Fpdf) error {
	pdf.AddUTF8FontFromBytes(exportPDFFontFamily, "", exportPDFRegularFont)
	pdf.AddUTF8FontFromBytes(exportPDFFontFamily, "B", exportPDFBoldFont)
	if err := pdf.Error(); err != nil {
		return fmt.Errorf("configure export pdf fonts: %w", err)
	}
	return nil
}
