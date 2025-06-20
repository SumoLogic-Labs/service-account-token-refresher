package tokenrefresher

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	testclient "k8s.io/client-go/kubernetes/fake"
	k8stesing "k8s.io/client-go/testing"
)

func TestTokenRefresher_ensureTarget(t *testing.T) {
	t.Run("ensureTarget() should fail if default token does not exist", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()

		err := r.ensureTarget()
		if err == nil {
			t.Error("ensureTarget() not failing if default token does not exist")
		}
	})

	t.Run("ensureTarget() should create a symlink to default token", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		defaultToken := "default_token_contents"
		safeWrite(r.DefaultTokenFile, defaultToken)

		err := r.ensureTarget()
		if err != nil {
			t.Errorf("ensureTarget() failed: %s", err.Error())
		}

		buf, err := os.ReadFile(r.TokenFile)
		if err != nil {
			t.Errorf("cannot read token file: %s", err.Error())
		}
		if string(buf) != defaultToken {
			t.Errorf("expected %s, got %s", defaultToken, string(buf))
		}
	})

	t.Run("ensureTarget() should not overwrite existing token", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		token := "new_token_contents"
		safeWrite(r.TokenFile, token)
		defaultToken := "default_token_contents"
		safeWrite(r.DefaultTokenFile, defaultToken)

		err := r.ensureTarget()
		if err != nil {
			t.Errorf("ensureTarget() failed: %s", err.Error())
		}

		buf, err := os.ReadFile(r.TokenFile)
		if err != nil {
			t.Errorf("cannot read token file: %s", err.Error())
		}
		if string(buf) != token {
			t.Errorf("expected %s, got %s", defaultToken, string(buf))
		}
	})
}

func TestTokenRefresher_waitForTrigger(t *testing.T) {
	t.Run("waitForTrigger() should return when signalled", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		safeWrite(r.TokenFile, getTokenWithExpiry(time.Hour*2))
		stopCh := make(chan struct{})
		retCh := make(chan struct{})

		go func() {
			r.waitForTrigger(stopCh)
			close(retCh)
		}()

		select {
		case <-retCh:
			t.Fatalf("waitForTrigger() returned pre-maturely without being signalled")
		case <-time.After(r.RefreshInterval * 2):
		}
		close(stopCh)
		select {
		case <-retCh:
		case <-time.After(r.RefreshInterval * 2):
			t.Errorf("waitForTrigger() did not return even after being signalled")
		}
	})

	t.Run("waitForTrigger() should return when token is invalidated", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		safeWrite(r.TokenFile, getTokenWithExpiry(time.Hour*2))
		stopCh := make(chan struct{})
		retCh := make(chan struct{})

		go func() {
			r.waitForTrigger(stopCh)
			close(retCh)
		}()

		select {
		case <-retCh:
			t.Fatalf("waitForTrigger() returned pre-maturely without being signalled")
		case <-time.After(r.RefreshInterval * 2):
		}
		safeWrite(r.TokenFile, getTokenWithExpiry(time.Minute))
		select {
		case <-retCh:
		case <-time.After(r.RefreshInterval * 2):
			t.Errorf("waitForTrigger() did not return even after token is invalidated")
		}
	})

	t.Run("waitForTrigger() should return immediately if token is invalid", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		stopCh := make(chan struct{})
		retCh := make(chan struct{})

		go func() {
			r.waitForTrigger(stopCh)
			close(retCh)
		}()

		select {
		case <-retCh:
		case <-time.After(r.RefreshInterval * 2):
			t.Errorf("waitForTrigger() did not return for an invalid token")
		}
	})

	t.Run("waitForTrigger() should return when shutdown file is detected", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		stopCh := make(chan struct{})
		retCh := make(chan struct{})
		safeWrite(r.TokenFile, getTokenWithExpiry(time.Hour*2))
		safeWrite(r.shutdownFile, "")

		go func() {
			r.waitForTrigger(stopCh)
			close(retCh)
		}()

		select {
		case <-retCh:
		case <-time.After(r.RefreshInterval * 2):
			t.Errorf("waitForTrigger() did not return on creation of shutdown file")
		}
		// The shutdown file should still exist
		if !r.shouldShutdown() {
			t.Errorf("waitForTrigger() deleted the shutdown file after consuming the signal")
		}
	})
}

func TestTokenRefresher_refresh(t *testing.T) {
	t.Run("refresh() should write a valid token to file", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		safeWrite(r.TokenFile, "")
		c := getFakeClient(r, false)

		err := r.refresh(c)
		if err != nil {
			t.Fatalf("refresh() did not create a valid token: %s", err.Error())
		}

		if !readTokenAndValidate(r.TokenFile, r.minExpiryDuration) {
			t.Fatalf("refresh() created an invalid token file")
		}
	})

	t.Run("refresh() should fail and skip updating token in case of errors", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		want := "this_string_should_not_be_overwritten"
		safeWrite(r.TokenFile, want)
		c := getFakeClient(r, true)

		err := r.refresh(c)
		if err == nil {
			t.Error("refresh() did not fail on error")
		}

		got, err := os.ReadFile(r.TokenFile)
		if err != nil {
			t.Errorf("unable to read token file: %s", err.Error())
		}
		if want != string(got) {
			t.Errorf("want: %s, got %s", want, string(got))
		}
	})
}

func TestTokenRefresher_refreshLoop(t *testing.T) {
	t.Run("refreshLoop() should exit when shutdown file exists", func(t *testing.T) {
		r, cleanup := setup()
		defer cleanup()
		safeWrite(r.TokenFile, "")
		c := getFakeClient(r, false)
		retCh := make(chan struct{})

		go func() {
			r.refreshLoop(c)
			close(retCh)
		}()

		select {
		case <-retCh:
			t.Fatalf("refreshLoop() returned pre-maturely without being shutdown")
		case <-time.After(r.RefreshInterval * 2):
		}
		safeWrite(r.shutdownFile, "")
		select {
		case <-retCh:
		case <-time.After(r.RefreshInterval * 2):
			t.Errorf("refreshLoop() did not return even after shutdown file was created")
		}
		// The shutdown file should be cleaned up
		if r.shouldShutdown() {
			t.Errorf("refreshLoop() did not delete the shutdown file on exit")
		}
	})
}

func setup() (*TokenRefresher, func()) {
	testDir, err := os.MkdirTemp(os.TempDir(), "token-refresher-test-tmp-*")
	if err != nil {
		panic(err.Error())
	}
	r := &TokenRefresher{
		DefaultTokenFile:   path.Join(testDir, "default_token"),
		TokenFile:          path.Join(testDir, "token"),
		ExpirationDuration: time.Hour * 2, // used to test if refresh() is sending this correctly to apiserver
		RefreshInterval:    time.Millisecond * 200,
		ShutdownInterval:   time.Millisecond * 200,
		Namespace:          "test-ns",
		ServiceAccount:     "test-sa",

		minExpiryDuration: time.Minute * 90,
		shutdownFile:      path.Join(testDir, ShutdownFile),
	}
	cleanup := func() {
		os.RemoveAll(testDir)
	}
	return r, cleanup
}

func getFakeClient(r *TokenRefresher, wantErr bool) *testclient.Clientset {
	ns, sa, exp := r.Namespace, r.ServiceAccount, r.ExpirationDuration
	c := testclient.NewSimpleClientset()
	c.PrependReactor("create", "serviceaccounts", func(action k8stesing.Action) (bool, runtime.Object, error) {
		if wantErr {
			return true, nil, fmt.Errorf("apiserver overloaded, could not create token")
		}
		act := action.(k8stesing.CreateActionImpl)
		ret := act.GetObject().DeepCopyObject().(*authv1.TokenRequest)
		if act.GetNamespace() != ns {
			return true, nil, fmt.Errorf("want ns: %s, got %s", ns, act.GetNamespace())
		}
		if act.Name != sa {
			return true, nil, fmt.Errorf("want sa: %s, got %s", sa, act.Name)
		}
		if *ret.Spec.ExpirationSeconds != int64(exp.Seconds()) {
			return true, nil, fmt.Errorf("want exp: %v, got %v", exp.Seconds(), *ret.Spec.ExpirationSeconds)
		}
		ret.Status.Token = getTokenWithExpiry(time.Duration(*ret.Spec.ExpirationSeconds) * time.Second)
		return true, ret, nil
	})
	return c
}
