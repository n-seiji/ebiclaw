package archiver

import (
	"fmt"
	"strconv"
	"strings"
)

func renderPromptRecordsTOON(records []promptRecord) string {
	var b strings.Builder
	b.WriteString("messages[")
	b.WriteString(strconv.Itoa(len(records)))
	b.WriteString("]{ts,role,chat,thread,sender,text}:\n")
	for _, rec := range records {
		fmt.Fprintf(
			&b,
			"  %s,%s,%s,%s,%s,%s\n",
			toonQuote(rec.Timestamp.UTC().Format(timeLayoutRFC3339)),
			toonQuote(rec.Role),
			toonQuote(rec.Chat),
			toonQuote(rec.Thread),
			toonQuote(rec.Sender),
			toonQuote(rec.Text),
		)
	}
	return b.String()
}

const timeLayoutRFC3339 = "2006-01-02T15:04:05Z"

func toonQuote(s string) string {
	return strconv.Quote(strings.ReplaceAll(s, "\r\n", "\n"))
}
