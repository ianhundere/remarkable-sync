package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"remarkable-sync/internal/convert"
	"remarkable-sync/internal/remarkable"

	"github.com/spf13/cobra"
)

var (
	// root command
	rootCmd = &cobra.Command{
		Use:   "remarkable-sync",
		Short: "Sync files between Obsidian and reMarkable",
		Long:  `A tool for syncing files between Obsidian vault and reMarkable tablet.`,
	}

	// global flags
	remarkableHost     string
	remarkableDir      string
	obsidianVault      string
	restartXochitl     bool
	quiet              bool
	purgeExceptPattern string

	// pdf flags
	pdfMargins    float64
	pdfFontSize   float64
	pdfMainFont   string
	pdfMonoFont   string
	pdfPageSize   string
	pdfColorLinks bool
	pdfTOC        bool
	pdfHighlight  bool

	// markdown flags
	mdHeaderAdjust int
	mdFrontmatter  bool
	mdCleanupText  bool
)

func init() {
	// add subcommands
	rootCmd.AddCommand(newObsidianCmd())
	rootCmd.AddCommand(newFromRemarkableCmd())
	rootCmd.AddCommand(newToRemarkableCmd())
	rootCmd.AddCommand(newCleanupCmd())

	// global flags
	rootCmd.PersistentFlags().StringVar(&remarkableHost, "host", "remarkable", "reMarkable tablet hostname/IP")
	rootCmd.PersistentFlags().StringVar(&remarkableDir, "remarkable-dir", "/home/root/.local/share/remarkable/xochitl", "reMarkable documents directory")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")
	rootCmd.PersistentFlags().BoolVarP(&restartXochitl, "restart", "r", true, "Restart xochitl after transfer")

	// pdf flags
	rootCmd.PersistentFlags().Float64Var(&pdfMargins, "pdf-margins", 20.0, "margins in mm")
	rootCmd.PersistentFlags().Float64Var(&pdfFontSize, "pdf-fontsize", 11.0, "base font size")
	rootCmd.PersistentFlags().StringVar(&pdfMainFont, "pdf-font", "Arial", "main font")
	rootCmd.PersistentFlags().StringVar(&pdfMonoFont, "pdf-monofont", "Courier", "monospace font")
	rootCmd.PersistentFlags().StringVar(&pdfPageSize, "pdf-pagesize", "A4", "page size")
	rootCmd.PersistentFlags().BoolVar(&pdfColorLinks, "pdf-colorlinks", true, "use colored links")
	rootCmd.PersistentFlags().BoolVar(&pdfTOC, "pdf-toc", true, "include table of contents")
	rootCmd.PersistentFlags().BoolVar(&pdfHighlight, "pdf-highlight", true, "highlight code blocks")

	// markdown flags
	rootCmd.PersistentFlags().IntVar(&mdHeaderAdjust, "md-header-adjust", 1, "adjust header levels")
	rootCmd.PersistentFlags().BoolVar(&mdFrontmatter, "md-frontmatter", true, "add yaml frontmatter")
	rootCmd.PersistentFlags().BoolVar(&mdCleanupText, "md-cleanup", true, "clean up extracted text")
}

func getPDFOptions() convert.PDFOptions {
	return convert.PDFOptions{
		Margins:    pdfMargins,
		FontSize:   pdfFontSize,
		MainFont:   pdfMainFont,
		MonoFont:   pdfMonoFont,
		PageSize:   pdfPageSize,
		ColorLinks: pdfColorLinks,
		TOC:        pdfTOC,
		Highlight:  pdfHighlight,
	}
}

func getMarkdownOptions() convert.MarkdownOptions {
	return convert.MarkdownOptions{
		HeaderLevelAdjust: mdHeaderAdjust,
		AddFrontmatter:    mdFrontmatter,
		CleanupText:       mdCleanupText,
	}
}

func log(format string, args ...interface{}) {
	if !quiet {
		fmt.Printf(format+"\n", args...)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newToRemarkableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "to-remarkable [files/directories...]",
		Short: "Transfer files to reMarkable tablet",
		Long:  `Transfer PDF and EPUB files to reMarkable tablet. If no arguments provided, transfers from Obsidian vault.`,
		RunE:  toRemarkableHandler,
	}
	return cmd
}

// process files in a directory or single file
func processFiles(path string, process func(string) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	if info.IsDir() {
		return filepath.Walk(path, func(filePath string, fileInfo os.FileInfo, err error) error {
			if err != nil || fileInfo.IsDir() {
				return err
			}
			return process(filePath)
		})
	}
	return process(path)
}

func toRemarkableHandler(cmd *cobra.Command, args []string) error {
	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to remarkable: %w", err)
	}
	defer client.Close()

	if restartXochitl {
		log("stopping xochitl...")
		if _, err := client.RunCommand("systemctl stop xochitl"); err != nil {
			return fmt.Errorf("failed to stop xochitl: %w", err)
		}
	}

	// process each path
	for _, path := range args {
		err := processFiles(path, func(filePath string) error {
			if !isSupported(filePath) {
				return fmt.Errorf("unsupported file type: %s", filePath)
			}
			return uploadFile(client, filePath)
		})
		if err != nil {
			log("warning: %v", err)
		}
	}

	if restartXochitl {
		log("restarting xochitl...")
		if _, err := client.RunCommand("systemctl restart xochitl"); err != nil {
			return fmt.Errorf("failed to restart xochitl: %w", err)
		}
	}

	return nil
}

func newFromRemarkableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "from-remarkable",
		Short: "Transfer files from reMarkable to Obsidian",
		Long:  `Download PDFs from reMarkable tablet and convert them to markdown in Obsidian vault.`,
		RunE:  fromRemarkableHandler,
	}
	return cmd
}

func fromRemarkableHandler(cmd *cobra.Command, args []string) error {
	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to reMarkable: %w", err)
	}
	defer client.Close()

	converter, err := convert.NewConverter()
	if err != nil {
		return fmt.Errorf("failed to create converter: %w", err)
	}
	defer converter.Close()

	// apply conversion options
	converter.SetMarkdownOptions(getMarkdownOptions())

	// create inbox directory
	inboxDir := filepath.Join(obsidianVault, "Inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		return fmt.Errorf("failed to create inbox directory: %w", err)
	}

	// get list of files from remarkable
	files, err := client.ListFiles()
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	for _, file := range files {
		log("Processing: %s", file)

		// skip if file already exists
		mdPath := filepath.Join(inboxDir, file+".md")
		if _, err := os.Stat(mdPath); err == nil {
			log("Skipping %s (already exists)", file)
			continue
		}

		// download pdf
		pdfPath, err := client.DownloadFile(file, file)
		if err != nil {
			log("Warning: failed to download %s: %v", file, err)
			continue
		}

		// convert to markdown
		if _, err := converter.PDFToMarkdown(pdfPath, inboxDir); err != nil {
			log("Warning: failed to convert %s: %v", file, err)
			continue
		}

		log("Successfully converted: %s", file)
	}

	return nil
}

func newObsidianCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obsidian [files/directories...]",
		Short: "Sync Obsidian vault with reMarkable",
		Long:  `Convert markdown files from Obsidian vault to PDF and upload them to reMarkable tablet.`,
		RunE:  obsidianHandler,
	}
	cmd.Flags().StringVar(&obsidianVault, "vault", os.ExpandEnv("$HOME/notes"), "Path to Obsidian vault")
	return cmd
}

func obsidianHandler(cmd *cobra.Command, args []string) error {
	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to remarkable: %w", err)
	}
	defer client.Close()

	converter, err := convert.NewConverter()
	if err != nil {
		return fmt.Errorf("failed to create converter: %w", err)
	}
	defer converter.Close()

	converter.SetOptions(getPDFOptions())

	if restartXochitl {
		log("stopping xochitl...")
		if _, err := client.RunCommand("systemctl stop xochitl"); err != nil {
			return fmt.Errorf("failed to stop xochitl: %w", err)
		}
	}

	// process provided paths or entire vault
	paths := args
	if len(paths) == 0 {
		paths = []string{obsidianVault}
	}

	for _, path := range paths {
		err := processFiles(path, func(filePath string) error {
			if !strings.HasSuffix(filePath, ".md") {
				return nil
			}
			return convertAndUpload(client, converter, filePath)
		})
		if err != nil {
			log("warning: %v", err)
		}
	}

	if restartXochitl {
		log("restarting xochitl...")
		if _, err := client.RunCommand("systemctl restart xochitl"); err != nil {
			return fmt.Errorf("failed to restart xochitl: %w", err)
		}
	}

	return nil
}

func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up files on reMarkable",
		Long:  `Remove files from reMarkable tablet, with option to preserve specific patterns.`,
		RunE:  cleanupHandler,
	}
	cmd.Flags().StringVar(&purgeExceptPattern, "except", "", "Pattern to preserve (e.g. 'Quick sheets|Notebook tutorial')")
	return cmd
}

func cleanupHandler(cmd *cobra.Command, args []string) error {
	if purgeExceptPattern == "" {
		return fmt.Errorf("--except pattern is required")
	}

	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to reMarkable: %w", err)
	}
	defer client.Close()

	// stop xochitl before cleanup
	if restartXochitl {
		log("Stopping xochitl...")
		if _, err := client.RunCommand("systemctl stop xochitl"); err != nil {
			return fmt.Errorf("failed to stop xochitl: %w", err)
		}
	}

	log("Cleaning up files except those matching: %s", purgeExceptPattern)
	if err := client.CleanupExcept(purgeExceptPattern); err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// Restart xochitl if needed
	if restartXochitl {
		log("Restarting xochitl...")
		if _, err := client.RunCommand("systemctl restart xochitl"); err != nil {
			return fmt.Errorf("failed to restart xochitl: %w", err)
		}
	}

	return nil
}

// helper functions
func isSupported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".pdf" || ext == ".epub"
}

func uploadFile(client *remarkable.Client, path string) error {
	log("Uploading: %s", path)
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return client.UploadFile(path, name)
}

func convertAndUpload(client *remarkable.Client, converter *convert.Converter, mdPath string) error {
	log("Converting and uploading: %s", mdPath)

	// convert to pdf
	pdfPath, err := converter.MarkdownToPDF(mdPath)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// send to remarkable
	name := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	if err := client.UploadFile(pdfPath, name); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	return nil
}
