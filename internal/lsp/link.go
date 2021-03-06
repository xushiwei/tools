// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/internal/lsp/protocol"
	"golang.org/x/tools/internal/lsp/source"
	"golang.org/x/tools/internal/span"
	"golang.org/x/tools/internal/telemetry/log"
	"golang.org/x/tools/internal/telemetry/tag"
)

func (s *Server) documentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	uri := span.NewURI(params.TextDocument.URI)
	view, err := s.session.ViewOf(uri)
	if err != nil {
		return nil, err
	}
	fh, err := view.Snapshot().GetFile(ctx, uri)
	if err != nil {
		return nil, err
	}
	if fh.Identity().Kind == source.Mod {
		return nil, nil
	}
	file, m, _, err := view.Session().Cache().ParseGoHandle(fh, source.ParseFull).Parse(ctx)
	if err != nil {
		return nil, err
	}
	var links []protocol.DocumentLink
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ImportSpec:
			target, err := strconv.Unquote(n.Path.Value)
			if err != nil {
				log.Error(ctx, "cannot unquote import path", err, tag.Of("Path", n.Path.Value))
				return false
			}
			if target == "" {
				return false
			}
			target = fmt.Sprintf("https://%s/%s", view.Options().LinkTarget, target)
			l, err := toProtocolLink(view, m, target, n.Path.Pos()+1, n.Path.End()-1)
			if err != nil {
				log.Error(ctx, "cannot initialize DocumentLink", err, tag.Of("Path", n.Path.Value))
				return false
			}
			links = append(links, l)
			return false
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return false
			}
			l, err := findLinksInString(view, n.Value, n.Pos(), m)
			if err != nil {
				log.Error(ctx, "cannot find links in string", err)
				return false
			}
			links = append(links, l...)
			return false
		}
		return true
	})

	for _, commentGroup := range file.Comments {
		for _, comment := range commentGroup.List {
			l, err := findLinksInString(view, comment.Text, comment.Pos(), m)
			if err != nil {
				log.Error(ctx, "cannot find links in comment", err)
				continue
			}
			links = append(links, l...)
		}
	}

	return links, nil
}

func findLinksInString(view source.View, src string, pos token.Pos, mapper *protocol.ColumnMapper) ([]protocol.DocumentLink, error) {
	var links []protocol.DocumentLink
	for _, urlIndex := range view.Options().URLRegexp.FindAllIndex([]byte(src), -1) {
		var target string
		start := urlIndex[0]
		end := urlIndex[1]
		startPos := token.Pos(int(pos) + start)
		endPos := token.Pos(int(pos) + end)
		target = src[start:end]
		l, err := toProtocolLink(view, mapper, target, startPos, endPos)
		if err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, nil
}

func toProtocolLink(view source.View, mapper *protocol.ColumnMapper, target string, start, end token.Pos) (protocol.DocumentLink, error) {
	spn, err := span.NewRange(view.Session().Cache().FileSet(), start, end).Span()
	if err != nil {
		return protocol.DocumentLink{}, err
	}
	rng, err := mapper.Range(spn)
	if err != nil {
		return protocol.DocumentLink{}, err
	}
	l := protocol.DocumentLink{
		Range:  rng,
		Target: target,
	}
	return l, nil
}
