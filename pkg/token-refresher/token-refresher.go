package tokenrefresher

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/SumoLogic-Labs/service-account-token-refresher/pkg/retry"
	"github.com/SumoLogic-Labs/service-account-token-refresher/pkg/ticker"

	v1 "k8s.io/api/authentication/v1"
	"k8s.io/client-go/kubernetes"
)

// ShutdownFile indicates to token refresher that it can exit gracefuly now and cleanup the file on exit.
// Must be in the same directory as TokenFile. Content does not matter.
const ShutdownFile = "shutdown"

type TokenRefresher struct {
	Namespace          string        `mapstructure:"namespace"`
	ServiceAccount     string        `mapstructure:"service_account"`
	KubeConfig         string        `mapstructure:"kubeconfig"`
	DefaultTokenFile   string        `mapstructure:"default_token_file"`
	TokenFile          string        `mapstructure:"token_file"`
	TokenAudience      []string      `mapstructure:"token_audience"`
	ExpirationDuration time.Duration `mapstructure:"expiration_duration"`
	RefreshInterval    time.Duration `mapstructure:"refresh_interval"`
	ShutdownInterval   time.Duration `mapstructure:"shutdown_interval"`
	Retryer            retry.Retryer `mapstructure:",squash"`

	minExpiryDuration time.Duration
	shutdownFile      string
}

func (r TokenRefresher) Run(stopCh <-chan struct{}) error {
	client, err := r.Init()
	if err != nil {
		return fmt.Errorf("unable to initialize: %w", err)
	}
	r.waitForTrigger(stopCh)
	r.refreshLoop(client)
	return nil
}

func (r *TokenRefresher) Init() (kubernetes.Interface, error) {
	r.minExpiryDuration = r.RefreshInterval + r.RefreshInterval/2
	r.shutdownFile = path.Join(path.Dir(r.TokenFile), ShutdownFile)
	fmt.Printf("Running TokenRefresher with config: %+v\n", *r)
	err := r.ensureTarget()
	if err != nil {
		return nil, err
	}
	return createKubeClient(r.KubeConfig)
}

func (r TokenRefresher) ensureTarget() error {
	_, err := os.Stat(r.DefaultTokenFile)
	if err != nil {
		return fmt.Errorf("unable to access default token at %s: %w", r.DefaultTokenFile, err)
	}
	_, err = os.Stat(r.TokenFile)
	if err == nil {
		fmt.Printf("Target already exists: %s\n", r.TokenFile)
		return nil
	}
	err = os.Symlink(r.DefaultTokenFile, r.TokenFile)
	if err != nil {
		return fmt.Errorf("unable to symlink %s -> %s: %w", r.TokenFile, r.DefaultTokenFile, err)
	}
	fmt.Printf("Created link: %s -> %s\n", r.TokenFile, r.DefaultTokenFile)
	return nil
}

// waitForTrigger blocks until it either receives a shutdown signal or detects an invalid token
// token-refresher spends most of its time here - waiting for the trigger
func (r TokenRefresher) waitForTrigger(stopCh <-chan struct{}) {
	fmt.Println("Waiting for shutdown signal and monitoring token expiry")
	ch := r.monitorToken(stopCh)
	for {
		select {
		case <-stopCh:
			fmt.Println("Shutdown signal received - Ignoring")
			return
		case msg := <-ch:
			fmt.Println(msg)
			return
		}
	}
}

func (r TokenRefresher) monitorToken(stopCh <-chan struct{}) <-chan string {
	ticker := ticker.NewTicker(r.RefreshInterval)
	ch := make(chan string)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !readTokenAndValidate(r.TokenFile, r.minExpiryDuration) {
					ch <- "Invalid/expired token detected"
					return
				}
				if r.shouldShutdown() {
					ch <- "Shutdown file detected while monitoring token"
					return
				}
			case <-stopCh:
				return
			}
		}
	}()
	return ch
}

func (r TokenRefresher) refreshLoop(client kubernetes.Interface) {
	fmt.Println("Starting refresh loop")
	fmt.Printf("Will refresh every %v\n", r.RefreshInterval)
	fmt.Printf("Will check for shutdown file every %v\n", r.ShutdownInterval)
	refreshTicker := ticker.NewTicker(r.RefreshInterval)
	shutdownTicker := ticker.NewTicker(r.ShutdownInterval)
	defer refreshTicker.Stop()
	defer shutdownTicker.Stop()
	for {
		select {
		case <-refreshTicker.C:
			err := r.Retryer.Do(func() (error, bool) {
				return r.refresh(client), true
			})
			if err != nil {
				fmt.Printf("unable to refresh token: %s\n", err.Error())
				continue
			}
			fmt.Println("Refreshed token")

		case <-shutdownTicker.C:
			if r.shouldShutdown() {
				fmt.Println("Shutdown signal detected")
				if err := os.Remove(r.shutdownFile); err != nil {
					fmt.Printf("unable to remove shutdown file: %s\n", err.Error())
				}
				return
			}
		}
	}
}

func (r TokenRefresher) refresh(client kubernetes.Interface) error {
	token, err := r.createToken(client)
	if err != nil {
		return err
	}
	if !isTokenValid(token, r.minExpiryDuration) {
		return fmt.Errorf("invalid token from server")
	}
	return safeWrite(r.TokenFile, token)
}

func (r TokenRefresher) createToken(client kubernetes.Interface) (string, error) {
	expSec := r.ExpirationDuration.Milliseconds() / 1000
	req := &v1.TokenRequest{
		Spec: v1.TokenRequestSpec{
			Audiences:         r.TokenAudience,
			ExpirationSeconds: &expSec,
		},
	}
	resp, err := createToken(client, r.Namespace, r.ServiceAccount, req)
	if err != nil {
		return "", fmt.Errorf("unable to create token: %w", err)
	}
	return resp.Status.Token, nil
}

func (r TokenRefresher) shouldShutdown() bool {
	_, err := os.Stat(r.shutdownFile)
	if err != nil {
		return false
	}
	fmt.Printf("Shutdown file detected at %s\n", r.shutdownFile)
	return true
}
