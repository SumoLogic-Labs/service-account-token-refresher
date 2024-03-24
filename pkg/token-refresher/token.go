package tokenrefresher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	v1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func createKubeClient(kubeconfig string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("no in-cluster config or kubeconfig found")
		}
	}
	return kubernetes.NewForConfig(config)
}

func createToken(client kubernetes.Interface, ns, sa string, req *v1.TokenRequest) (*v1.TokenRequest, error) {
	return client.CoreV1().
		ServiceAccounts(ns).
		CreateToken(context.TODO(), sa, req, metav1.CreateOptions{})
}

func readTokenAndValidate(tokenFile string, minExp time.Duration) bool {
	b, err := os.ReadFile(tokenFile)
	if err != nil {
		fmt.Printf("unable to read file %s: %s\n", tokenFile, err.Error())
		return false
	}
	return isTokenValid(string(b), minExp)
}

// isTokenValid checks if the `exp` key in the claims of the jwt is valid for at least the given duration
func isTokenValid(token string, minExp time.Duration) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		fmt.Println("invalid token")
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		fmt.Printf("unable to decode token: %s\n", err.Error())
		return false
	}
	var claims map[string]interface{}
	err = json.Unmarshal(data, &claims)
	if err != nil {
		fmt.Printf("unable to decode json: %s\n", err.Error())
		return false
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		fmt.Printf("exp not a number: %v", claims["exp"])
		return false
	}
	expiresAt := time.Unix(int64(exp), 0)
	expiresIn := time.Until(expiresAt)
	if expiresIn < 0 {
		fmt.Printf("token has expired at %v (%v ago)\n", expiresAt, -expiresIn)
		return false
	}
	if expiresIn < minExp {
		fmt.Printf("token too old, expires at %v (in %v)\n", expiresAt, expiresIn)
		return false
	}
	// fmt.Printf("token is valid, expires at %v (in %v)\n", expiresAt, expiresIn)
	return true
}

// safeWrite first writes to a temp file and then switches it with the target file atomically by renaming
func safeWrite(filename, data string) error {
	tmpFilename, err := writeTemp(filename, data)
	if err != nil {
		return err
	}
	// Cleanup temp file in case rename fails
	defer os.Remove(tmpFilename)
	err = os.Rename(tmpFilename, filename)
	if err != nil {
		return fmt.Errorf("unable to rename %s to %s: %w", tmpFilename, filename, err)
	}
	return nil
}

func writeTemp(filename, data string) (string, error) {
	f, err := os.CreateTemp(path.Dir(filename), path.Base(filename))
	if err != nil {
		return "", fmt.Errorf("unable to create file: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(data)
	if err != nil {
		return f.Name(), fmt.Errorf("unable to write to file %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
