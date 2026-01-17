package kerbetor

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/asabellico/kerbetor/pkg/kerbetor"
	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var verbose bool
var rootCmd = &cobra.Command{
	Use:   "kerbetor [flags] <remote url>",
	Short: "kerbetor - a download manager for the dark web",
	Long: `kerbetor is a download manager for the dark web. 
	
It is a CLI tool that can be used to download files from TOR concurrently, using multiple TOR circuits.`,
	Args: func(cmd *cobra.Command, args []string) error {
		inputFile, _ := cmd.Flags().GetString("input-file")
		if inputFile != "" {
			if len(args) != 0 {
				return fmt.Errorf("when --input-file is set, do not pass a remote url argument")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires a single remote url argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if verbose {
			logrus.SetLevel(logrus.DebugLevel)
		}

		output, _ := cmd.Flags().GetString("output")
		inputFile, _ := cmd.Flags().GetString("input-file")
		chunkSizeStr, _ := cmd.Flags().GetString("chunk-size")
		chunkCount, _ := cmd.Flags().GetUint("chunks")
		if chunkCount > 0 && cmd.Flags().Changed("chunk-size") {
			logrus.Error("Cannot set both --chunks and --chunk-size")
			os.Exit(1)
		}
		chunkSize, err := humanize.ParseBytes(chunkSizeStr)
		if err != nil {
			logrus.Error("Cannot parse chunk size:", err)
			os.Exit(1)
		}
		maxConcurrentDownloads, _ := cmd.Flags().GetUint("parallel-downloads")
		numTorCircuits, _ := cmd.Flags().GetUint("tor-circuits")

		if chunkCount > 0 {
			logrus.Info("Chunk count: ", chunkCount)
			logrus.Info("Chunk size: auto (from --chunks)")
		} else {
			logrus.Info("Chunk size: ", humanize.Bytes(chunkSize))
		}
		logrus.Info("Max concurrent downloads: ", maxConcurrentDownloads)
		logrus.Info("Number of TOR circuits: ", numTorCircuits)

		if inputFile != "" {
			remoteUrls, err := readUrlsFromFile(inputFile)
			if err != nil {
				logrus.Error("Cannot read input file: ", err)
				os.Exit(1)
			}
			if len(remoteUrls) == 0 {
				logrus.Error("Input file contains no URLs")
				os.Exit(1)
			}

			outputDir, useOutputAsFile, err := resolveOutputForBatch(output, len(remoteUrls))
			if err != nil {
				logrus.Error(err)
				os.Exit(1)
			}

			downloaded := 0
			downloadErrors := 0
			for idx, remoteUrl := range remoteUrls {
				outputPath := output
				if outputDir != "" {
					outputPath = buildOutputPath(outputDir, remoteUrl, idx)
				} else if outputPath == "" || !useOutputAsFile {
					outputPath = buildOutputPath("", remoteUrl, idx)
				}
				logrus.Info("Downloading ", remoteUrl, ". Writing output to: ", outputPath)
				errDownload := kerbetor.ConcurrentFileDownload(remoteUrl, outputPath, chunkSize, maxConcurrentDownloads, numTorCircuits, chunkCount)
				if errDownload != nil {
					logrus.Error(errDownload)
					downloadErrors++
				} else {
					downloaded++
				}
			}
			logDownloadSummary(len(remoteUrls), downloaded, downloadErrors)
			return
		}

		remoteUrl := args[0]
		if output == "" {
			output = buildOutputPath("", remoteUrl, 0)
		}
		logrus.Info("Downloading ", remoteUrl, ". Writing output to: ", output)
		downloaded := 0
		downloadErrors := 0
		errDownload := kerbetor.ConcurrentFileDownload(remoteUrl, output, chunkSize, maxConcurrentDownloads, numTorCircuits, chunkCount)
		if errDownload != nil {
			logrus.Error(errDownload)
			downloadErrors++
		} else {
			downloaded++
		}
		logDownloadSummary(1, downloaded, downloadErrors)
	},
}

func init() {
	rootCmd.PersistentFlags().StringP("output", "o", "", "downloaded file output path")
	rootCmd.PersistentFlags().UintP("parallel-downloads", "p", 3, "number of parallel downloads")
	rootCmd.PersistentFlags().UintP("tor-circuits", "c", 1, "number of TOR circuits to use")
	rootCmd.PersistentFlags().StringP("chunk-size", "s", "100mb", "chunk size")
	rootCmd.PersistentFlags().UintP("chunks", "n", 0, "number of chunks (overrides --chunk-size)")
	rootCmd.PersistentFlags().StringP("input-file", "i", "", "path to a text file with one URL per line")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func readUrlsFromFile(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

func resolveOutputForBatch(output string, urlCount int) (string, bool, error) {
	if output == "" {
		return "", false, nil
	}

	info, err := os.Stat(output)
	if err != nil {
		if os.IsNotExist(err) {
			if urlCount > 1 {
				if err := os.MkdirAll(output, 0755); err != nil {
					return "", false, fmt.Errorf("cannot create output directory: %s", err)
				}
				return output, false, nil
			}
			return "", true, nil
		}
		return "", false, fmt.Errorf("cannot access output path: %s", err)
	}
	if !info.IsDir() {
		if urlCount > 1 {
			return "", false, fmt.Errorf("output path must be a directory when using --input-file with multiple URLs")
		}
		return "", true, nil
	}
	return output, false, nil
}

func buildOutputPath(outputDir string, remoteUrl string, index int) string {
	fileName := defaultOutputName(remoteUrl, index)
	if outputDir == "" {
		return fileName
	}
	return filepath.Join(outputDir, fileName)
}

func defaultOutputName(remoteUrl string, index int) string {
	parsedURL, err := url.Parse(remoteUrl)
	if err == nil {
		base := path.Base(parsedURL.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	lastSlash := strings.LastIndex(remoteUrl, "/")
	if lastSlash >= 0 && lastSlash+1 < len(remoteUrl) {
		return remoteUrl[lastSlash+1:]
	}
	return fmt.Sprintf("download-%d", index+1)
}

func logDownloadSummary(total int, downloaded int, errors int) {
	logrus.Info("Download summary: total=", total, ", downloaded=", downloaded, ", errors=", errors)
}
