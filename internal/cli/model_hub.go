package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// searchItem is the JSON view of a Hub search result.
type searchItem struct {
	ID          string   `json:"id"`
	Author      string   `json:"author,omitempty"`
	Downloads   int      `json:"downloads"`
	Likes       int      `json:"likes"`
	GGUF        bool     `json:"gguf"`
	PipelineTag string   `json:"pipelineTag,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// repoFile / repoInfoView are the JSON view of repo metadata.
type repoFile struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"sizeBytes"`
}
type repoInfoView struct {
	ID    string     `json:"id"`
	Tags  []string   `json:"tags,omitempty"`
	Files []repoFile `json:"files"`
}

func init() {
	var limit int
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search HuggingFace Hub for models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitOnErr(cmd.ErrOrStderr(),
				searchHub(cmd.OutOrStdout(), buildHFClient(), args[0], limit, jsonOut))
		},
	}
	searchCmd.Flags().IntVar(&limit, "limit", 20, "maximum number of results")
	modelCmd.AddCommand(searchCmd)

	infoCmd := &cobra.Command{
		Use:   "info <repo-id>",
		Short: "Show HuggingFace repo metadata and file listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitOnErr(cmd.ErrOrStderr(),
				showRepoInfo(cmd.OutOrStdout(), buildHFClient(), args[0], jsonOut))
		},
	}
	modelCmd.AddCommand(infoCmd)
}

func searchHub(out io.Writer, hub hubClient, query string, limit int, asJSON bool) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("search query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	results, err := hub.Search(context.Background(), query, limit)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if asJSON {
		items := make([]searchItem, 0, len(results))
		for _, r := range results {
			items = append(items, searchItem{
				ID: r.ID, Author: r.Author, Downloads: r.Downloads, Likes: r.Likes,
				GGUF: r.HasGGUFTag(), PipelineTag: r.PipelineTag, Tags: r.Tags,
			})
		}
		return emitJSON(out, items)
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "no results")
		return nil
	}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		gguf := "-"
		if r.HasGGUFTag() {
			gguf = "yes"
		}
		rows = append(rows, []string{
			clip(r.ID, 48), strconv.Itoa(r.Downloads), strconv.Itoa(r.Likes), gguf, dashOr(r.PipelineTag),
		})
	}
	printTable(out, []string{"ID", "DOWNLOADS", "LIKES", "GGUF", "PIPELINE"}, rows)
	return nil
}

func showRepoInfo(out io.Writer, hub hubClient, repoID string, asJSON bool) error {
	if strings.TrimSpace(repoID) == "" {
		return errors.New("repo id is required")
	}
	info, err := hub.RepoInfo(context.Background(), repoID)
	if err != nil {
		return fmt.Errorf("repo info: %w", err)
	}
	if info == nil {
		return fmt.Errorf("repo not found: %s", repoID)
	}
	if asJSON {
		files := make([]repoFile, 0, len(info.Siblings))
		for _, s := range info.Siblings {
			files = append(files, repoFile{Filename: s.RFilename, SizeBytes: s.Size})
		}
		return emitJSON(out, repoInfoView{ID: info.ID, Tags: info.Tags, Files: files})
	}
	fmt.Fprintf(out, "Repo:  %s\n", info.ID)
	if len(info.Tags) > 0 {
		fmt.Fprintf(out, "Tags:  %s\n", strings.Join(info.Tags, ", "))
	}
	fmt.Fprintln(out)
	if len(info.Siblings) == 0 {
		fmt.Fprintln(out, "no files")
		return nil
	}
	rows := make([][]string, 0, len(info.Siblings))
	for _, s := range info.Siblings {
		rows = append(rows, []string{s.RFilename, humanBytes(s.Size)})
	}
	printTable(out, []string{"FILENAME", "SIZE"}, rows)
	return nil
}
