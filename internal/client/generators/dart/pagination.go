package dart

import (
	"fmt"
	"strings"
)

// pagingParams maps a query parameter name to the PageParams field that
// feeds it, and the Dart type that field has.
var pagingParams = map[string][2]string{
	"cursor":    {"cursor", "String"},
	"limit":     {"limit", "int"},
	"page_size": {"limit", "int"},
	"pageSize":  {"limit", "int"},
	"per_page":  {"limit", "int"},
	"offset":    {"offset", "int"},
	"page":      {"page", "int"},
}

// paginated is one list operation the generator can walk page by page.
type paginated struct {
	op       *operation
	method   string
	call     string
	item     string
	items    string
	cursor   string
	hasMore  string
	moreNull bool
	paging   map[string]string
}

// planPagination finds the operations whose response is a class with an
// items list (data, items or results) and whose query takes a cursor, page
// or offset. An operation with any other required query parameter is left
// out: the walker has no value to give it. So is one that requires a paging
// parameter itself, because PageParams fields are optional and there is no
// honest default to send.
func planPagination(ops []*operation, paths map[*operation]string, reg *registry) []paginated {
	var out []paginated

	// The extension's members must not collide with a member of RestClient
	// itself: the class member would win and the stream would be unreachable.
	names := copySet(restReserved)

	for _, path := range paths {
		root, _, _ := strings.Cut(path, ".")
		names[root] = true
	}

	for _, op := range ops {
		if !isReadMethod(op.ep.Method) || op.response != "json" {
			continue
		}

		m := reg.modelNamed(op.responseType.name)
		if m == nil || m.kind != kindClass || len(m.decls) == 0 {
			continue
		}

		cls, isClass := m.decls[0].(*classDecl)
		if !isClass {
			continue
		}

		p := paginated{op: op, call: paths[op], paging: map[string]string{}}

		for _, f := range cls.fields {
			switch strings.ToLower(f.wire) {
			case "data", "items", "results":
				if strings.HasPrefix(f.typ.name, "List<") && !f.nullable {
					p.items = f.member
					p.item = strings.TrimSuffix(strings.TrimPrefix(f.typ.name, "List<"), ">")
				}
			case "next_cursor", "nextcursor", "next":
				if f.typ.name == "String" {
					p.cursor = f.member
				}
			case "has_more", "hasmore":
				if f.typ.name == "bool" {
					p.hasMore, p.moreNull = f.member, f.nullable
				}
			}
		}

		ok := p.items != ""
		walks := false

		for _, prm := range op.params {
			if prm.in != "query" {
				continue
			}

			if target, isPaging := pagingParams[prm.wire]; isPaging && prm.typ.name == target[1] {
				p.paging[prm.member] = target[0]
				walks = walks || target[0] != "limit"

				if prm.required {
					ok = false
				}

				continue
			}

			if prm.required {
				ok = false
			}
		}

		if !ok || !walks || p.call == "" {
			continue
		}

		p.method = uniqueNames([]string{op.key}, func(s string) string { return memberIdent(s, restReserved) + "Paginated" }, names, false)[0]
		out = append(out, p)
	}

	return out
}

// renderPagination renders lib/src/pagination.dart: the page walker, and an
// extension on RestClient with a Stream-returning variant per paginated
// list operation.
func renderPagination(pages []paginated, reg *registry) string {
	var b strings.Builder

	var ext strings.Builder

	if len(pages) > 0 {
		ext.WriteString("\n/// Paginated variants of list operations.\n")
		ext.WriteString("extension RestClientPagination on RestClient {\n")

		for i, p := range pages {
			if i > 0 {
				ext.WriteString("\n")
			}

			var sig, args []string

			// Names a required parameter may already hold: the walker's own
			// two locals and the first segment of the accessor path.
			taken := map[string]bool{}
			root, _, _ := strings.Cut(p.call, ".")

			for _, prm := range p.op.params {
				if _, isPaging := p.paging[prm.member]; !isPaging && prm.required {
					taken[prm.member] = true
				}
			}

			paramsName := freeName("params", taken)
			pageName := freeName("p", taken)

			call := p.call
			if taken[root] {
				call = "this." + call
			}

			for _, prm := range p.op.params {
				if field, isPaging := p.paging[prm.member]; isPaging {
					args = append(args, fmt.Sprintf("%s: %s.%s", prm.member, pageName, field))

					continue
				}

				if prm.required {
					sig = append(sig, "required "+prm.typ.name+" "+prm.member)
					args = append(args, prm.member+": "+prm.member)
				}
			}

			sig = append(sig, "PageParams "+paramsName+" = const PageParams()")

			fmt.Fprintf(&ext, "  /// Every item of `%s %s`, across pages.\n", strings.ToUpper(p.op.ep.Method), strings.ReplaceAll(p.op.ep.Path, "`", "'"))
			fmt.Fprintf(&ext, "  Stream<%s> %s({%s}) =>\n", p.item, p.method, strings.Join(sig, ", "))
			fmt.Fprintf(&ext, "      paginateAll((%s) async {\n", pageName)
			fmt.Fprintf(&ext, "        final page = await %s(%s);\n", call, strings.Join(args, ", "))

			extra := ""
			if p.cursor != "" {
				extra += ", nextCursor: page." + p.cursor
			}

			if p.hasMore != "" {
				if p.moreNull {
					extra += ", hasMore: page." + p.hasMore + " ?? false"
				} else {
					extra += ", hasMore: page." + p.hasMore
				}
			}

			fmt.Fprintf(&ext, "        return Page(page.%s%s);\n", p.items, extra)
			fmt.Fprintf(&ext, "      }, initial: %s);\n", paramsName)
		}

		ext.WriteString("}\n")
	}

	b.WriteString(generatedHeader)

	if len(pages) > 0 {
		text := ext.String()

		var dartImports []string
		if strings.Contains(text, "Uint8List") {
			dartImports = append(dartImports, "import 'dart:typed_data';")
		}

		local := reg.importsFor(text, "models/")
		local = append(local, "import 'rest.dart';")

		if shown := usedSymbols(text, supportSymbols); len(shown) > 0 {
			local = append(local, "import 'support.dart' show "+strings.Join(shown, ", ")+";")
		}

		b.WriteString(importBlock(dartImports, nil, local))
	}

	b.WriteString(paginationHelpers)
	b.WriteString(ext.String())

	return b.String()
}

// freeName returns base, or base with a numeric suffix when taken has it.
func freeName(base string, taken map[string]bool) string {
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s%d", base, n)
	}

	return name
}

// modelNamed finds the component whose Dart type is name.
func (r *registry) modelNamed(name string) *componentModel {
	for _, m := range r.models {
		if m.dartName == name {
			return m
		}
	}

	return nil
}

const paginationHelpers = `
/// Where the next page starts.
final class PageParams {
  /// Creates page parameters.
  const PageParams({this.cursor, this.limit, this.offset, this.page});

  /// An opaque cursor from the previous page.
  final String? cursor;

  /// The page size.
  final int? limit;

  /// The number of items to skip.
  final int? offset;

  /// A 1-based page number.
  final int? page;
}

/// One page of results.
final class Page<T> {
  /// Creates a page.
  const Page(this.items, {this.nextCursor, this.hasMore = false});

  /// The items on this page.
  final List<T> items;

  /// The cursor for the next page, when the server returned one.
  final String? nextCursor;

  /// Whether the server reported more pages.
  final bool hasMore;
}

/// Streams every item across pages, fetching lazily as the stream is read.
Stream<T> paginateAll<T>(
  Future<Page<T>> Function(PageParams params) fetchPage, {
  PageParams initial = const PageParams(),
}) async* {
  var params = initial;
  while (true) {
    final page = await fetchPage(params);
    yield* Stream.fromIterable(page.items);
    if (page.nextCursor case final cursor? when cursor.isNotEmpty) {
      params = PageParams(cursor: cursor, limit: params.limit);
    } else if (page.hasMore && params.page != null) {
      params = PageParams(page: params.page! + 1, limit: params.limit);
    } else if (page.hasMore && params.offset != null && params.limit != null) {
      params = PageParams(offset: params.offset! + params.limit!, limit: params.limit);
    } else {
      return;
    }
  }
}

/// Collects every item across pages, stopping at [maxItems] when given.
Future<List<T>> collectAll<T>(
  Future<Page<T>> Function(PageParams params) fetchPage, {
  PageParams initial = const PageParams(),
  int? maxItems,
}) async {
  final items = <T>[];
  await for (final item in paginateAll(fetchPage, initial: initial)) {
    items.add(item);
    if (maxItems != null && items.length >= maxItems) break;
  }
  return items;
}
`
