package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SumoLogic-Labs/service-account-token-refresher/pkg/signals"
	tokenrefresher "github.com/SumoLogic-Labs/service-account-token-refresher/pkg/token-refresher"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/util/homedir"
)

type config struct {
	tokenrefresher.TokenRefresher `mapstructure:",squash"`
}

var conf *config

var rootCmd = &cobra.Command{
	Use:   "token-refresher",
	Short: "Automatic token refresher for terminating pods",
	Long:  `A sidecar which starts auto-refreshing the service account token when the default one is close to expiry or container receives a shutdown signal.`,
	Run: func(cmd *cobra.Command, args []string) {
		stopCh := signals.SignalShutdown()
		refresher := conf.TokenRefresher
		if err := refresher.Run(stopCh); err != nil {
			fmt.Printf("unable to run: %s", err.Error())
			os.Exit(2)
		}
		fmt.Println("Exiting")
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// The flag names must match those from conf.TokenRefresher
	rootCmd.Flags().StringP("namespace", "n", "", "current namespace")
	rootCmd.Flags().StringP("service_account", "s", "", "name of service account to issue token for")
	rootCmd.Flags().String("default_token_file", "/var/run/secrets/eks.amazonaws.com/serviceaccount/token", "path to default service account token file")
	rootCmd.Flags().String("token_file", "/var/run/secrets/token-refresher/token", "path to self-managed service account token file")
	rootCmd.Flags().StringSlice("token_audience", []string{"sts.amazonaws.com"}, "comma separated token audience")
	rootCmd.Flags().Duration("expiration_duration", time.Hour*2, "token expiry duration")
	rootCmd.Flags().Duration("refresh_interval", time.Hour*1, "token refresh interval")
	rootCmd.Flags().Duration("shutdown_interval", time.Minute*1, "token refresher shutdown check interval")
	rootCmd.Flags().Int("max_attempts", 3, "max retries on token refresh failure")
	rootCmd.Flags().Duration("sleep", time.Second*20, "sleep duration between retries")

	if home := homedir.HomeDir(); home != "" {
		rootCmd.Flags().String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		rootCmd.Flags().String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	viper.BindPFlags(rootCmd.LocalFlags())
}

func initConfig() {
	viper.AutomaticEnv() // read in upper-cased env vars corresponding to above CLI flags
	conf = new(config)
	err := viper.Unmarshal(conf)
	if err != nil {
		fmt.Printf("unable to decode into config struct, %v", err)
	}
}
