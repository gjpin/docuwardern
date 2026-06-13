package markdown

import (
	"context"
	"fmt"
	"io"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

type Converter interface {
	Convert(ctx context.Context, pageURL string, html io.Reader) (string, error)
}

type HTMLConverter struct{}

func (HTMLConverter) Convert(ctx context.Context, pageURL string, input io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := md.ConvertReader(input, converter.WithDomain(pageURL))
	if err != nil {
		return "", fmt.Errorf("convert HTML to Markdown: %w", err)
	}
	return string(output), nil
}
