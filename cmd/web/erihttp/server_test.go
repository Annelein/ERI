package erihttp

import (
	"bytes"
	"net/http"
	"reflect"
	"testing"

	"github.com/Dynom/ERI/cmd/web/config"
)

func TestBuildHTTPServer(t *testing.T) {
	type args struct {
		mux      http.Handler
		config   config.Config
		handlers []func(h http.Handler) http.Handler
	}
	tests := []struct {
		name          string
		args          args
		wantLogWriter string
		want          *http.Server
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logWriter := &bytes.Buffer{}
			got := BuildHTTPServer(tt.args.mux, tt.args.config, logWriter, tt.args.handlers...)
			if gotLogWriter := logWriter.String(); gotLogWriter != tt.wantLogWriter {
				t.Errorf("BuildHTTPServer() gotLogWriter = %v, want %v", gotLogWriter, tt.wantLogWriter)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildHTTPServer() = %v, want %v", got, tt.want)
			}
		})
	}
}