package dart

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// streamIntents maps an IR intent to its StreamIntent value.
var streamIntents = map[string]string{"upsert": "upsert", "patch": "patch", "evict": "evict"}

// renderOps renders lib/src/ops.dart: the operation, entity, stream and
// security tables. Pure data, const throughout.
func renderOps(spec *client.APISpec, config client.GeneratorConfig, ops []*operation, naming codecNaming) (string, []string) {
	var (
		body     strings.Builder
		warnings []string
	)

	codecFiles := map[string]bool{}
	codec := func(id string) string {
		ref, ok := naming.byID[id]
		if id == "" || !ok {
			return ""
		}

		codecFiles[fmt.Sprintf("import 'codecs/%s.dart';", ref.file)] = true

		return ref.constant
	}

	sorted := append([]*operation(nil), ops...)
	sortOps(sorted)

	for _, op := range sorted {
		fmt.Fprintf(&body, "\n/// `%s %s`\n", strings.ToUpper(op.row.Method), strings.ReplaceAll(op.row.Path, "`", "'"))
		fmt.Fprintf(&body, "const %s = OperationMeta(\n", op.constant)
		fmt.Fprintf(&body, "  id: %s,\n", dartString(op.id))
		fmt.Fprintf(&body, "  method: %s,\n", dartString(strings.ToUpper(op.row.Method)))
		fmt.Fprintf(&body, "  path: %s,\n", dartString(op.row.Path))

		if op.row.Entity != "" {
			fmt.Fprintf(&body, "  entity: %s,\n", dartString(op.row.Entity))
		}

		if op.row.RootType != "" {
			fmt.Fprintf(&body, "  rootType: %s,\n", dartString(op.row.RootType))
		}

		if op.row.StaleTime > 0 {
			fmt.Fprintf(&body, "  staleTime: Duration(milliseconds: %d),\n", op.row.StaleTime)
		}

		if len(op.row.Provides) > 0 {
			fmt.Fprintf(&body, "  provides: %s,\n", dartStringList(op.row.Provides))
		}

		if len(op.row.Invalidates) > 0 {
			fmt.Fprintf(&body, "  invalidates: %s,\n", dartStringList(op.row.Invalidates))
		}

		if len(op.row.Security) > 0 {
			fmt.Fprintf(&body, "  security: %s,\n", dartStringList(op.row.Security))
		}

		if c := codec(op.row.BodyCodec); c != "" {
			fmt.Fprintf(&body, "  bodyCodec: %s,\n", c)
		}

		if c := codec(op.row.ResponseCodec); c != "" {
			fmt.Fprintf(&body, "  responseCodec: %s,\n", c)
		}

		if op.row.Idempotent {
			body.WriteString("  idempotent: true,\n")
		}

		body.WriteString(");\n")
	}

	body.WriteString("\n/// Every operation, keyed by [OperationMeta.id].\n")
	body.WriteString("const Map<String, OperationMeta> operations = {")

	if len(sorted) > 0 {
		body.WriteString("\n")

		for _, op := range sorted {
			fmt.Fprintf(&body, "  %s: %s,\n", dartString(op.id), op.constant)
		}
	}

	body.WriteString("};\n")

	body.WriteString("\n/// How to identify and descend each named type. A row with no idField is a\n")
	body.WriteString("/// signpost: walked for its fields, never stored.\n")
	body.WriteString("const EntitySchema entities = {")

	rows := entityRows(spec, config)
	if len(rows) > 0 {
		body.WriteString("\n")

		for _, row := range rows {
			var args []string

			if row.idField != "" {
				args = append(args, "idField: "+dartString(row.idField))
			}

			if len(row.fields) > 0 {
				var fields []string
				for _, prop := range sortedKeys(row.fields) {
					fields = append(fields, dartString(prop)+": "+dartString(row.fields[prop]))
				}

				args = append(args, "fields: {"+strings.Join(fields, ", ")+"}")
			}

			fmt.Fprintf(&body, "  %s: EntityMeta(%s),\n", dartString(row.name), strings.Join(args, ", "))
		}
	}

	body.WriteString("};\n")

	used := map[string]bool{"OperationMeta": true, "EntityMeta": len(rows) > 0, "EntitySchema": true, "StreamBinding": true}

	body.WriteString("\n/// What each stream message does to the cache.\n")
	body.WriteString("const List<StreamBinding> streams = [")

	streams := streamRows(spec)
	if len(streams) > 0 {
		body.WriteString("\n")
	}

	for _, s := range streams {
		if s.Kind == "duplex" {
			used["DuplexStreamBinding"] = true

			fmt.Fprintf(&body, "  DuplexStreamBinding(channel: %s, send: %s, receive: %s),\n",
				dartString(s.Channel), dartString(s.Send), dartString(s.Receive))

			continue
		}

		intent, ok := streamIntents[s.Intent]
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"stream %s message %q declares intent %q, which is not upsert, patch or evict; the binding is skipped",
				s.Channel, s.Message, s.Intent))

			continue
		}

		used["EntityStreamBinding"] = true
		used["StreamIntent"] = true

		body.WriteString("  EntityStreamBinding(\n")
		fmt.Fprintf(&body, "    channel: %s,\n", dartString(s.Channel))
		fmt.Fprintf(&body, "    message: %s,\n", dartString(s.Message))
		fmt.Fprintf(&body, "    entity: %s,\n", dartString(s.Entity))
		fmt.Fprintf(&body, "    intent: StreamIntent.%s,\n", intent)

		if len(s.Invalidates) > 0 {
			fmt.Fprintf(&body, "    invalidates: %s,\n", dartStringList(s.Invalidates))
		}

		if c := codec(s.Decode); c != "" {
			fmt.Fprintf(&body, "    decode: %s,\n", c)
		}

		body.WriteString("  ),\n")
	}

	body.WriteString("];\n")

	body.WriteString("\n/// Every security scheme the document declares.\n")
	body.WriteString("const Map<String, SecurityScheme> securitySchemes = {")

	if len(spec.Security) > 0 {
		body.WriteString("\n")
	}

	used["SecurityScheme"] = true

	for _, s := range spec.Security {
		args := []string{"type: " + dartString(s.Type)}

		if s.Scheme != "" {
			args = append(args, "scheme: "+dartString(s.Scheme))
		}

		if s.ParamName != "" {
			args = append(args, "name: "+dartString(s.ParamName))
		}

		if s.In != "" {
			args = append(args, "location: "+dartString(s.In))
		}

		fmt.Fprintf(&body, "  %s: SecurityScheme(%s),\n", dartString(s.Key), strings.Join(args, ", "))
	}

	body.WriteString("};\n")

	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString("//\n// Operation, entity, stream and security tables. Pure data: the runtime in\n")
	b.WriteString("// package:forge_client reads them, and a runtime fix never needs this file\n// regenerated.\n")

	var shown []string

	for _, name := range sortedKeys(used) {
		if used[name] {
			shown = append(shown, name)
		}
	}

	b.WriteString(importBlock(nil,
		[]string{"import 'package:forge_client/forge_client.dart'\n    show " + strings.Join(shown, ", ") + ";"},
		sortedKeys(codecFiles)))
	b.WriteString("\nexport 'sync.dart' show sync;\n")
	b.WriteString(body.String())

	return b.String(), warnings
}

// renderSync renders lib/src/sync.dart, one row per sync-backed entity. A
// row needs both a pull and a push endpoint: an entity that cannot do both
// is not something a sync source can own, so it is reported and left as a
// plain REST entity.
func renderSync(spec *client.APISpec) (string, []string) {
	var (
		b        strings.Builder
		warnings []string
		rows     []client.SyncDecl
	)

	for _, decl := range spec.Sync {
		if decl.Pull == "" || decl.Push == "" {
			warnings = append(warnings, fmt.Sprintf(
				"x-forge-sync: entity %q has no %s, so it is left out of the sync table and stays a plain REST entity",
				decl.Entity, missingSyncRoles(decl)))

			continue
		}

		rows = append(rows, decl)
	}

	b.WriteString(generatedHeader)
	b.WriteString("\nimport 'package:forge_client/forge_client.dart' show SyncDeclaration;\n")
	b.WriteString("\n/// Entities whose records come from a sync source rather than REST.\n")
	b.WriteString("const List<SyncDeclaration> sync = [")

	if len(rows) > 0 {
		b.WriteString("\n")
	}

	for _, d := range rows {
		b.WriteString("  SyncDeclaration(\n")
		fmt.Fprintf(&b, "    protocol: %s,\n", dartString(d.Protocol))
		fmt.Fprintf(&b, "    entity: %s,\n", dartString(d.Entity))

		// An absent table means one table per dataset, chosen at runtime.
		if d.Table != "" {
			fmt.Fprintf(&b, "    table: %s,\n", dartString(d.Table))
		}

		fmt.Fprintf(&b, "    pull: %s,\n", dartString(d.Pull))
		fmt.Fprintf(&b, "    push: %s,\n", dartString(d.Push))

		if d.Stream != "" {
			fmt.Fprintf(&b, "    stream: %s,\n", dartString(d.Stream))
		}

		if d.Socket != "" {
			fmt.Fprintf(&b, "    socket: %s,\n", dartString(d.Socket))
		}

		if d.Dataset != "" {
			fmt.Fprintf(&b, "    dataset: %s,\n", dartString(d.Dataset))
		}

		b.WriteString("  ),\n")
	}

	b.WriteString("];\n")

	return b.String(), warnings
}

// missingSyncRoles names the endpoints a sync row lacks, both when it has
// neither.
func missingSyncRoles(d client.SyncDecl) string {
	switch {
	case d.Pull == "" && d.Push == "":
		return "pull or push endpoint"
	case d.Pull == "":
		return "pull endpoint"
	}

	return "push endpoint"
}

// sortOps orders operations by their table id, which is how ops.dart lists
// them.
func sortOps(ops []*operation) {
	for i := 1; i < len(ops); i++ {
		for j := i; j > 0 && ops[j].id < ops[j-1].id; j-- {
			ops[j], ops[j-1] = ops[j-1], ops[j]
		}
	}
}
