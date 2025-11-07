package convert

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/ledongthuc/pdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

// pdf generation config
type PDFOptions struct {
	Margins    float64
	FontSize   float64
	MainFont   string
	MonoFont   string
	PageSize   string
	ColorLinks bool
	TOC        bool
	Highlight  bool
}

// default pdf options
func DefaultPDFOptions() PDFOptions {
	return PDFOptions{
		Margins:    20.0,
		FontSize:   11.0,
		MainFont:   "Arial",
		MonoFont:   "Courier",
		PageSize:   "A4",
		ColorLinks: true,
		TOC:        true,
		Highlight:  true,
	}
}

// markdown generation config
type MarkdownOptions struct {
	HeaderLevelAdjust int  // bump header levels by this amount
	AddFrontmatter    bool // add yaml frontmatter
	CleanupText       bool // cleanup extracted text
}

// default markdown options
func DefaultMarkdownOptions() MarkdownOptions {
	return MarkdownOptions{
		HeaderLevelAdjust: 1, // # becomes ##
		AddFrontmatter:    true,
		CleanupText:       true,
	}
}

type Converter struct {
	TempDir   string
	options   PDFOptions
	mdOptions MarkdownOptions
}

func NewConverter() (*Converter, error) {
	tmpDir, err := os.MkdirTemp("", "remarkable-convert-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	return &Converter{
		TempDir:   tmpDir,
		options:   DefaultPDFOptions(),
		mdOptions: DefaultMarkdownOptions(),
	}, nil
}

func (c *Converter) SetOptions(options PDFOptions) {
	c.options = options
}

func (c *Converter) SetMarkdownOptions(options MarkdownOptions) {
	c.mdOptions = options
}

func (c *Converter) Close() error {
	return os.RemoveAll(c.TempDir)
}

func (c *Converter) setupPDF() *gofpdf.Fpdf {
	pdf := gofpdf.New("P", "mm", c.options.PageSize, "")
	pdf.SetMargins(c.options.Margins, c.options.Margins, c.options.Margins)
	pdf.AddPage()
	return pdf
}

func (c *Converter) processYAML(pdf *gofpdf.Fpdf, content []byte) error {
	var data interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	pdf.SetFont(c.options.MonoFont, "", c.options.FontSize)
	yamlStr := fmt.Sprintf("```yaml\n%s\n```", string(content))
	pdf.MultiCell(0, 5, yamlStr, "", "", false)
	return nil
}

func (c *Converter) processConfig(pdf *gofpdf.Fpdf, content []byte) error {
	pdf.SetFont(c.options.MonoFont, "", c.options.FontSize)
	pdf.MultiCell(0, 5, string(content), "", "", false)
	return nil
}

// pdfRenderer renders goldmark AST to gofpdf
type pdfRenderer struct {
	pdf          *gofpdf.Fpdf
	options      PDFOptions
	source       []byte
	listDepth    int
	fontStack    []string // track font style (B, I, BI, "")
	baseLeftMargin float64   // original left margin
}

func (r *pdfRenderer) pushFont(style string) {
	r.fontStack = append(r.fontStack, style)
	r.pdf.SetFont(r.options.MainFont, style, r.options.FontSize)
}

func (r *pdfRenderer) popFont() {
	if len(r.fontStack) > 0 {
		r.fontStack = r.fontStack[:len(r.fontStack)-1]
	}
	if len(r.fontStack) > 0 {
		r.pdf.SetFont(r.options.MainFont, r.fontStack[len(r.fontStack)-1], r.options.FontSize)
	} else {
		r.pdf.SetFont(r.options.MainFont, "", r.options.FontSize)
	}
}

func (r *pdfRenderer) currentFont() string {
	if len(r.fontStack) > 0 {
		return r.fontStack[len(r.fontStack)-1]
	}
	return ""
}

func (r *pdfRenderer) render(node ast.Node, entering bool) ast.WalkStatus {
	switch n := node.(type) {
	case *ast.Document:
		if entering {
			r.pdf.SetFont(r.options.MainFont, "", r.options.FontSize)
		}

	case *ast.Heading:
		if entering {
			r.pdf.Ln(6)  // Space before heading
			size := r.options.FontSize + float64(6-n.Level)*2
			r.pdf.SetFont(r.options.MainFont, "B", size)
		} else {
			r.pdf.SetFont(r.options.MainFont, "", r.options.FontSize)
			r.pdf.Ln(5)  // Space after heading
		}

	case *ast.Paragraph:
		if !entering {
			r.pdf.Ln(4)
		}

	case *ast.TextBlock:
		// TextBlock is used inside list items - don't add extra spacing
		// Just let the child text nodes render

	case *ast.List:
		if entering {
			r.listDepth++
			r.pdf.Ln(2)
			// Set left margin for this list level
			r.pdf.SetLeftMargin(r.baseLeftMargin + float64(r.listDepth)*6)
		} else {
			r.listDepth--
			// Restore previous margin
			r.pdf.SetLeftMargin(r.baseLeftMargin + float64(r.listDepth)*6)
			if r.listDepth == 0 {
				r.pdf.Ln(2)
			}
		}

	case *ast.ListItem:
		if entering {
			// Move to start of line with proper indent
			lMargin, _, _, _ := r.pdf.GetMargins()
			r.pdf.SetX(lMargin)

			// Write bullet/number - use simple ASCII bullet for compatibility
			if n.Parent().(*ast.List).IsOrdered() {
				r.pdf.Cell(5, 5, "1.")
			} else {
				r.pdf.Cell(5, 5, "*")  // Use asterisk instead of Unicode bullet
			}
			r.pdf.Write(5, " ")
		} else {
			r.pdf.Ln(5)
		}

	case *ast.Emphasis:
		if entering {
			// Level 1 = italic (*text*), Level 2 = bold (**text**)
			if n.Level == 2 {
				r.pushFont("B")
			} else {
				r.pushFont("I")
			}
		} else {
			r.popFont()
		}

	case *ast.CodeSpan:
		if entering {
			r.pdf.SetFont(r.options.MonoFont, "", r.options.FontSize-1)
			if r.options.Highlight {
				r.pdf.SetFillColor(240, 240, 240)
			}
			code := string(n.Text(r.source))
			r.pdf.Write(5, code)
			r.pdf.SetFont(r.options.MainFont, r.currentFont(), r.options.FontSize)
			return ast.WalkSkipChildren
		}

	case *ast.FencedCodeBlock:
		if entering {
			r.pdf.Ln(3)
			r.pdf.SetFont(r.options.MonoFont, "", r.options.FontSize-1)
			if r.options.Highlight {
				r.pdf.SetFillColor(245, 245, 245)
			}

			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				line := lines.At(i)
				r.pdf.MultiCell(0, 5, string(line.Value(r.source)), "", "", r.options.Highlight)
			}

			r.pdf.SetFont(r.options.MainFont, r.currentFont(), r.options.FontSize)
			r.pdf.SetFillColor(255, 255, 255)
			r.pdf.Ln(3)
			return ast.WalkSkipChildren
		}

	case *ast.Link:
		if entering {
			if r.options.ColorLinks {
				r.pdf.SetTextColor(0, 0, 255)
			}
		} else {
			r.pdf.SetTextColor(0, 0, 0)
		}

	case *ast.Text:
		if entering {
			txt := string(n.Segment.Value(r.source))

			// Handle soft line breaks
			if n.SoftLineBreak() {
				txt += " "
			}

			r.pdf.Write(5, txt)
		}

	case *ast.String:
		if entering {
			r.pdf.Write(5, string(n.Value))
		}
	}

	return ast.WalkContinue
}

func (c *Converter) processMarkdown(pdf *gofpdf.Fpdf, content []byte) error {
	md := goldmark.New(
		goldmark.WithExtensions(),
	)

	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	// Get initial left margin
	lMargin, _, _, _ := pdf.GetMargins()

	renderer := &pdfRenderer{
		pdf:            pdf,
		options:        c.options,
		source:         content,
		fontStack:      []string{},
		baseLeftMargin: lMargin,
	}

	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		return renderer.render(node, entering), nil
	})

	return err
}

func (c *Converter) MarkdownToPDF(mdPath string) (string, error) {
	title := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	pdfPath := filepath.Join(c.TempDir, title+".pdf")

	content, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("failed to read markdown: %w", err)
	}

	pdf := c.setupPDF()

	// Don't add title separately - it's in the markdown as H1
	// Just process the content

	// process content based on file type
	ext := strings.ToLower(filepath.Ext(mdPath))
	var processErr error

	switch ext {
	case ".yml", ".yaml":
		processErr = c.processYAML(pdf, content)
	case ".conf", ".ini", ".config":
		processErr = c.processConfig(pdf, content)
	default:
		processErr = c.processMarkdown(pdf, content)
	}

	if processErr != nil {
		return "", processErr
	}

	// save pdf
	if err := pdf.OutputFileAndClose(pdfPath); err != nil {
		return "", fmt.Errorf("failed to create pdf: %w", err)
	}

	return pdfPath, nil
}

// cleanupMarkdownText improves the extracted text quality
func (c *Converter) cleanupMarkdownText(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	var inCodeBlock bool

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" && len(result) > 0 && result[len(result)-1] == "" {
			continue
		}

		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
		} else if !inCodeBlock {
			switch {
			case strings.HasPrefix(line, "#"):
				headerLevel := strings.Count(line, "#") + c.mdOptions.HeaderLevelAdjust
				line = strings.Repeat("#", headerLevel) + " " + strings.TrimSpace(strings.TrimLeft(line, "#"))
			case strings.HasPrefix(line, "* "), strings.HasPrefix(line, "- "):
				line = "- " + strings.TrimSpace(line[2:])
			case regexp.MustCompile(`^\d+\.\s`).MatchString(line):
				if parts := strings.SplitN(line, ".", 2); len(parts) == 2 {
					line = parts[0] + "." + strings.TrimSpace(parts[1])
				}
			}
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

func (c *Converter) PDFToMarkdown(pdfPath string, targetDir string) (string, error) {
	title := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	mdPath := filepath.Join(targetDir, title+".md")

	// open pdf file
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf: %w", err)
	}
	defer f.Close()

	var content strings.Builder

	// add yaml frontmatter if enabled
	if c.mdOptions.AddFrontmatter {
		frontmatter := fmt.Sprintf("---\ntitle: %s\nsource: remarkable\ndate: %s\n---\n\n",
			title, time.Now().Format("2006-01-02"))
		content.WriteString(frontmatter)
	}

	// extract text
	textReader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract text: %w", err)
	}

	// read and process text
	textBytes, err := io.ReadAll(textReader)
	if err != nil {
		return "", fmt.Errorf("failed to read text: %w", err)
	}

	extractedText := string(textBytes)

	// clean up the text if enabled
	if c.mdOptions.CleanupText {
		extractedText = c.cleanupMarkdownText(extractedText)
	}

	content.WriteString(extractedText)

	// write markdown file
	if err := os.WriteFile(mdPath, []byte(content.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write markdown: %w", err)
	}

	return mdPath, nil
}
