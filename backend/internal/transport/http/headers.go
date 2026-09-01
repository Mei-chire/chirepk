package httpapi

import (
	"fmt"
	"net/url"
)

func contentDispositionFilename(filename string) string {
	return fmt.Sprintf(`attachment; filename="chirepk-schedule.xlsx"; filename*=UTF-8''%s`, url.PathEscape(filename))
}
