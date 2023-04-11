package kerbetor

import (
	"os"
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
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if verbose {
			logrus.SetLevel(logrus.DebugLevel)
		}

		remoteUrl := args[0]
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			// set output to remoteUrl filename
			output = remoteUrl[strings.LastIndex(remoteUrl, "/")+1:]
		}

		chunkSizeStr, _ := cmd.Flags().GetString("chunk-size")
		chunkSize, err := humanize.ParseBytes(chunkSizeStr)
		if err != nil {
			logrus.Error("Cannot parse chunk size:", err)
			os.Exit(1)
		}
		maxConcurrentDownloads, _ := cmd.Flags().GetUint("parallel-downloads")
		numTorCircuits, _ := cmd.Flags().GetUint("tor-circuits")

		logrus.Info("Downloading ", remoteUrl, ". Writing output to: ", output)
		logrus.Info("Chunk size: ", humanize.Bytes(chunkSize))
		logrus.Info("Max concurrent downloads: ", maxConcurrentDownloads)
		logrus.Info("Number of TOR circuits: ", numTorCircuits)
		errDownload := kerbetor.ConcurrentFileDownload(remoteUrl, output, chunkSize, maxConcurrentDownloads, numTorCircuits)
		if errDownload != nil {
			logrus.Error(errDownload)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringP("output", "o", "", "downloaded file output path")
	rootCmd.PersistentFlags().UintP("parallel-downloads", "p", 3, "number of parallel downloads")
	rootCmd.PersistentFlags().UintP("tor-circuits", "c", 1, "number of TOR circuits to use")
	rootCmd.PersistentFlags().StringP("chunk-size", "s", "100mb", "chunk size")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
