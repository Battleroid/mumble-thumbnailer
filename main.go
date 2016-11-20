package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/layeh/gumble/gumble"
	"github.com/layeh/gumble/gumbleutil"
	"github.com/nfnt/resize"
	"golang.org/x/net/html"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	_ "net/http"
	"strings"
)

// TODO: CLI params needed for size adjustment
func main() {
	messages := make(chan string)
	found := make(chan string)
	done := make(chan bool, 1)

	gumbleutil.Main(gumbleutil.AutoBitrate, gumbleutil.Listener{
		Connect: func(e *gumble.ConnectEvent) {
			// start processor queue
			go processor(messages, found, done, e.Client)
		},
		TextMessage: func(e *gumble.TextMessageEvent) {
			// send new message for processing if it contains protocol
			if strings.Index(e.Message, "http") != -1 {
				messages <- e.Message
			}
		},
		Disconnect: func(e *gumble.DisconnectEvent) {
			// finished, send done signal
			done <- true
		},
	})

	defer close(messages)
	defer close(found)
}

func processor(messages chan string, found chan string, done chan bool, client *gumble.Client) {

	extensions := []string{
		"jpeg",
		"jpg",
		"png",
		"gif",
	}

	// for processing messages
	go func() {
		for {
			job := <-messages
			doneFinding := make(chan bool, 1)
			go findLinks(job, found, doneFinding)
		}
	}()

	// for thumbnail links
	go func() {
		for {
			// TODO: Should probably move the bulk of this to a separate function(s)
			link := <-found
			log.Println("Found:", link)

			// split string by last period
			parts := strings.Split(link, ".")
			e := strings.ToLower(parts[len(parts)-1])

			// check if extension is appropriate
			good := inSlice(e, extensions)
			if !good {
				log.Fatal("Not in extension list")
			}
			format := "image/" + e

			// download photo
			resp, err := http.Get(link)
			defer resp.Body.Close()
			if err != nil {
				log.Fatal("Failed to fetch image")
			}

			// resize photo to about 300px longest side
			var photo image.Image
			switch {
			case e == "jpeg" || e == "jpg":
				photo, err = jpeg.Decode(resp.Body)
			case e == "png":
				photo, err = png.Decode(resp.Body)
			case e == "gif":
				photo, err = gif.Decode(resp.Body)
			}
			if err != nil {
				log.Fatal("Error decoding image:", err)
			}

			// TODO: Max file size should be parameter
			// TODO: Need to check image size before thumbnailing

			// resize and encode to jpeg
			// TODO: Thumbnail params should be adjustable
			m := resize.Thumbnail(300, 300, photo, resize.Lanczos3)
			buf := new(bytes.Buffer)
			err = jpeg.Encode(buf, m, nil)
			if err != nil {
				log.Fatal("Error encoding image to JPEG")
			}
			b64 := html.EscapeString(base64.StdEncoding.EncodeToString(buf.Bytes()))
			payload := fmt.Sprintf("<img src=\"data:%s;base64,%s\"/>", format, b64)

			// send to channel
			client.Self.Channel.Send(payload, false)
		}
	}()

	// finished, disconnect entirely
	<-done
}

func inSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func findLinks(message string, links chan string, doneFinding chan bool) {
	z := html.NewTokenizer(bytes.NewBufferString(message))

	defer func() {
		// finished processing message
		doneFinding <- true
	}()

	for {
		token := z.Next()

		switch {
		case token == html.ErrorToken:
			// end of message, return
			return
		case token == html.StartTagToken:
			t := z.Token()

			// check if anchor
			isAnchor := t.Data == "a"
			if !isAnchor {
				continue
			}

			// check if proper link
			ok, url := getLink(t)
			if !ok {
				continue
			}

			// check if protocol is present
			hasHttp := strings.Index(url, "http") == 0
			if hasHttp {
				links <- url
			}
		}
	}
}

func getLink(t html.Token) (ok bool, link string) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			// found anchor destination, return
			link = a.Val
			ok = true
		}
	}
	return
}
