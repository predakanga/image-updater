package pkg

import (
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
)

func logError(w http.ResponseWriter, err error, errStr string) {
	logrus.WithError(err).Warn(errStr)
	w.WriteHeader(500)
	_, _ = io.WriteString(w, errStr)
}
