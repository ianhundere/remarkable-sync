package convert

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"github.com/jung-kurt/gofpdf"
	"github.com/ledongthuc/pdf"
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

func (c *Converter) processMarkdown(pdf *gofpdf.Fpdf, content []byte) error {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse(content)

	var inCodeBlock bool
	pdf.SetFont(c.options.MainFont, "", c.options.FontSize)

	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}

		switch n := node.(type) {
		case *ast.Heading:
			pdf.SetFont(c.options.MainFont, "B", 16-float64(n.Level))
			pdf.Ln(5)
		case *ast.Text:
			if inCodeBlock {
				pdf.SetFont(c.options.MonoFont, "", c.options.FontSize)
			}
			pdf.MultiCell(0, 5, string(n.Literal), "", "", inCodeBlock && c.options.Highlight)
			if inCodeBlock {
				pdf.SetFont(c.options.MainFont, "", c.options.FontSize)
			}
		case *ast.CodeBlock:
			inCodeBlock = true
			pdf.SetFont(c.options.MonoFont, "", c.options.FontSize)
			if c.options.Highlight {
				pdf.SetFillColor(245, 245, 245)
			}
			pdf.MultiCell(0, 5, string(n.Literal), "", "", c.options.Highlight)
			pdf.SetFont(c.options.MainFont, "", c.options.FontSize)
			inCodeBlock = false
		case *ast.Link:
			if c.options.ColorLinks {
				pdf.SetTextColor(0, 0, 255)
			}
		case *ast.Paragraph:
			pdf.Ln(5)
		case *ast.List:
			pdf.Ln(3)
		case *ast.ListItem:
			pdf.Write(5, "• ")
		}
		return ast.GoToNext
	})

	return nil
}

func (c *Converter) MarkdownToPDF(mdPath string) (string, error) {
	title := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	pdfPath := filepath.Join(c.TempDir, title+".pdf")

	content, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("failed to read markdown: %w", err)
	}

	pdf := c.setupPDF()

	// add title
	pdf.SetFont(c.options.MainFont, "B", c.options.FontSize+4)
	pdf.Cell(0, 10, title)
	pdf.Ln(15)

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
