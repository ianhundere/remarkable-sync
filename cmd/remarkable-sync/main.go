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
	forceOverwrite     bool
	purgeExceptPattern string
	folderName         string
	dryRun             bool

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
	rootCmd.AddCommand(newRemoveCmd())

	// global flags - used across multiple commands
	rootCmd.PersistentFlags().StringVar(&remarkableHost, "host", "remarkable", "reMarkable tablet hostname/IP")
	rootCmd.PersistentFlags().StringVar(&remarkableDir, "remarkable-dir", "/home/root/.local/share/remarkable/xochitl", "reMarkable documents directory")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")
	rootCmd.PersistentFlags().BoolVarP(&restartXochitl, "restart", "r", true, "Restart xochitl after transfer")
	rootCmd.PersistentFlags().BoolVarP(&forceOverwrite, "force", "f", false, "Overwrite existing files without prompting")
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
	cmd.Flags().StringVar(&folderName, "folder", "", "Upload files to this folder (creates if doesn't exist)")
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
	if len(args) == 0 {
		return fmt.Errorf("at least one file or directory path is required")
	}

	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to remarkable: %w", err)
	}
	defer client.Close()

	if err := stopXochitl(client); err != nil {
		return err
	}

	// handles folder creation if --folder flag is provided
	var parentUUID string
	if folderName != "" {
		log("Ensuring folder '%s' exists...", folderName)
		parentUUID, err = client.EnsureFolder(folderName)
		if err != nil {
			return fmt.Errorf("failed to ensure folder: %w", err)
		}
		log("Using folder UUID: %s", parentUUID)
	}

	// process each path
	for _, path := range args {
		err := processFiles(path, func(filePath string) error {
			if !isSupported(filePath) {
				// skips unsupported files silently
				return nil
			}
			return uploadFile(client, filePath, parentUUID)
		})
		if err != nil {
			log("warning: %v", err)
		}
	}

	return restartXochitlService(client)
}

func newFromRemarkableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "from-remarkable",
		Short: "Transfer files from reMarkable to Obsidian",
		Long:  `Download PDFs from reMarkable tablet and convert them to markdown in Obsidian vault.`,
		RunE:  fromRemarkableHandler,
	}

	// vault path
	cmd.Flags().StringVar(&obsidianVault, "vault", os.ExpandEnv("$HOME/notes"), "Path to Obsidian vault")

	// markdown conversion options
	cmd.Flags().IntVar(&mdHeaderAdjust, "md-header-adjust", 1, "adjust header levels")
	cmd.Flags().BoolVar(&mdFrontmatter, "md-frontmatter", true, "add yaml frontmatter")
	cmd.Flags().BoolVar(&mdCleanupText, "md-cleanup", true, "clean up extracted text")

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
		log("Processing: %s", file.Name)

		// skip if file already exists
		mdPath := filepath.Join(inboxDir, file.Name+".md")
		if _, err := os.Stat(mdPath); err == nil {
			log("Skipping %s (already exists)", file.Name)
			continue
		}

		// download pdf
		pdfPath, err := client.DownloadFile(file.UUID, file.Name)
		if err != nil {
			log("Warning: failed to download %s: %v", file.Name, err)
			continue
		}

		// convert to markdown
		if _, err := converter.PDFToMarkdown(pdfPath, inboxDir); err != nil {
			log("Warning: failed to convert %s: %v", file.Name, err)
			continue
		}

		log("Successfully converted: %s", file.Name)
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
	cmd.Flags().StringVar(&folderName, "folder", "", "Upload files to this folder (creates if doesn't exist)")

	// pdf conversion options
	cmd.Flags().Float64Var(&pdfMargins, "pdf-margins", 20.0, "margins in mm")
	cmd.Flags().Float64Var(&pdfFontSize, "pdf-fontsize", 11.0, "base font size")
	cmd.Flags().StringVar(&pdfMainFont, "pdf-font", "Arial", "main font")
	cmd.Flags().StringVar(&pdfMonoFont, "pdf-monofont", "Courier", "monospace font")
	cmd.Flags().StringVar(&pdfPageSize, "pdf-pagesize", "A4", "page size")
	cmd.Flags().BoolVar(&pdfColorLinks, "pdf-colorlinks", true, "use colored links")
	cmd.Flags().BoolVar(&pdfTOC, "pdf-toc", true, "include table of contents")
	cmd.Flags().BoolVar(&pdfHighlight, "pdf-highlight", true, "highlight code blocks")

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

	if err := stopXochitl(client); err != nil {
		return err
	}

	// handles folder creation if --folder flag is provided
	var parentUUID string
	if folderName != "" {
		log("Ensuring folder '%s' exists...", folderName)
		parentUUID, err = client.EnsureFolder(folderName)
		if err != nil {
			return fmt.Errorf("failed to ensure folder: %w", err)
		}
		log("Using folder UUID: %s", parentUUID)
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
			return convertAndUpload(client, converter, filePath, parentUUID)
		})
		if err != nil {
			log("warning: %v", err)
		}
	}

	return restartXochitlService(client)
}

func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up files on reMarkable",
		Long:  `Remove files from reMarkable tablet, with option to preserve specific patterns.`,
		RunE:  cleanupHandler,
	}
	cmd.Flags().StringVar(&purgeExceptPattern, "except", "", "Pattern to preserve (e.g. 'Quick sheets|Notebook tutorial')")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would be deleted without actually deleting")
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

	// first run a dry-run to preview changes
	log("Analyzing files on reMarkable...")
	result, err := client.CleanupExcept(purgeExceptPattern, true)
	if err != nil {
		return fmt.Errorf("failed to analyze files: %w", err)
	}

	// display preview
	log("\n=== CLEANUP PREVIEW ===")
	log("\nFiles to PRESERVE (%d):", len(result.PreservedFiles))
	if len(result.PreservedFiles) == 0 {
		log("  (none)")
	} else {
		for _, file := range result.PreservedFiles {
			log("  ✓ %s", file.Name)
		}
	}

	log("\nFiles to DELETE (%d):", len(result.DeletedFiles))
	if len(result.DeletedFiles) == 0 {
		log("  (none)")
		log("\nNo files to delete. Exiting.")
		return nil
	}
	for _, file := range result.DeletedFiles {
		log("  ✗ %s", file.Name)
	}
	log("")

	// if dry-run mode, stop here
	if dryRun {
		log("DRY RUN: No files were actually deleted.")
		return nil
	}

	// confirmation prompt unless forced
	if !forceOverwrite {
		fmt.Printf("\nAre you sure you want to delete %d file(s)? [y/N]: ", len(result.DeletedFiles))
		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			log("Cleanup cancelled.")
			return nil
		}
	}

	if err := stopXochitl(client); err != nil {
		return err
	}

	log("Deleting files...")
	result, err = client.CleanupExcept(purgeExceptPattern, false)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	if err := restartXochitlService(client); err != nil {
		return err
	}

	log("\n✓ Successfully deleted %d file(s) and preserved %d file(s)", len(result.DeletedFiles), len(result.PreservedFiles))
	return nil
}

func newRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [filename]",
		Short: "Remove a single file from reMarkable",
		Long:  `Remove a specific file from reMarkable tablet by its visible name.`,
		Args:  cobra.ExactArgs(1),
		RunE:  removeHandler,
	}
	return cmd
}

func removeHandler(cmd *cobra.Command, args []string) error {
	fileName := args[0]

	client, err := remarkable.NewClient(remarkableHost, remarkableDir)
	if err != nil {
		return fmt.Errorf("failed to connect to reMarkable: %w", err)
	}
	defer client.Close()

	// checks if file exists first
	exists, err := client.FileExists(fileName)
	if err != nil {
		return fmt.Errorf("failed to check if file exists: %w", err)
	}

	if !exists {
		log("File '%s' not found on reMarkable", fileName)
		return nil
	}

	if err := stopXochitl(client); err != nil {
		return err
	}

	log("Removing file: %s", fileName)
	if err := client.DeleteFileByName(fileName); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	if err := restartXochitlService(client); err != nil {
		return err
	}

	log("Successfully removed: %s", fileName)
	return nil
}

// helper functions
func stopXochitl(client *remarkable.Client) error {
	if !restartXochitl {
		return nil
	}
	log("Stopping xochitl...")
	if _, err := client.RunCommand("systemctl stop xochitl"); err != nil {
		return fmt.Errorf("failed to stop xochitl: %w", err)
	}
	return nil
}

func restartXochitlService(client *remarkable.Client) error {
	if !restartXochitl {
		return nil
	}
	log("Restarting xochitl...")
	if _, err := client.RunCommand("systemctl restart xochitl"); err != nil {
		return fmt.Errorf("failed to restart xochitl: %w", err)
	}
	return nil
}

func isSupported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".pdf" || ext == ".epub"
}

func uploadFile(client *remarkable.Client, path string, parentUUID string) error {
	log("Uploading: %s", path)
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if parentUUID != "" {
		return client.UploadFile(path, name, forceOverwrite, parentUUID)
	}
	return client.UploadFile(path, name, forceOverwrite)
}

func convertAndUpload(client *remarkable.Client, converter *convert.Converter, mdPath string, parentUUID string) error {
	log("Converting and uploading: %s", mdPath)

	// convert to pdf
	pdfPath, err := converter.MarkdownToPDF(mdPath)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// send to remarkable
	name := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	if parentUUID != "" {
		return client.UploadFile(pdfPath, name, forceOverwrite, parentUUID)
	}
	return client.UploadFile(pdfPath, name, forceOverwrite)
}
