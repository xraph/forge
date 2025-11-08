package cli

import (
	"fmt"
	"io"
	"strings"
)

// GenerateBashCompletion generates bash completion script.
func GenerateBashCompletion(cli CLI, w io.Writer) error {
	script := bashCompletionTemplate(cli)
	_, err := w.Write([]byte(script))

	return err
}

// GenerateZshCompletion generates zsh completion script.
func GenerateZshCompletion(cli CLI, w io.Writer) error {
	script := zshCompletionTemplate(cli)
	_, err := w.Write([]byte(script))

	return err
}

// GenerateFishCompletion generates fish completion script.
func GenerateFishCompletion(cli CLI, w io.Writer) error {
	script := fishCompletionTemplate(cli)
	_, err := w.Write([]byte(script))

	return err
}

// bashCompletionTemplate generates a bash completion script.
func bashCompletionTemplate(cli CLI) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# bash completion for %s\n\n", cli.Name()))
	sb.WriteString(fmt.Sprintf("_%s_completion() {\n", cli.Name()))
	sb.WriteString("    local cur prev commands\n")
	sb.WriteString("    COMPREPLY=()\n")
	sb.WriteString("    cur=\"${COMP_WORDS[COMP_CWORD]}\"\n")
	sb.WriteString("    prev=\"${COMP_WORDS[COMP_CWORD-1]}\"\n\n")

	// Generate command list
	sb.WriteString("    commands=\"")

	for i, cmd := range cli.Commands() {
		if i > 0 {
			sb.WriteString(" ")
		}

		sb.WriteString(cmd.Name())
	}

	sb.WriteString("\"\n\n")

	// Completion logic
	sb.WriteString("    if [[ ${COMP_CWORD} -eq 1 ]]; then\n")
	sb.WriteString("        COMPREPLY=($(compgen -W \"${commands}\" -- ${cur}))\n")
	sb.WriteString("        return 0\n")
	sb.WriteString("    fi\n\n")

	sb.WriteString("    case \"${prev}\" in\n")

	for _, cmd := range cli.Commands() {
		if len(cmd.Flags()) > 0 {
			sb.WriteString(fmt.Sprintf("        %s)\n", cmd.Name()))
			sb.WriteString("            local flags=\"")

			for i, flag := range cmd.Flags() {
				if i > 0 {
					sb.WriteString(" ")
				}

				sb.WriteString("--" + flag.Name())

				if flag.ShortName() != "" {
					sb.WriteString(" -" + flag.ShortName())
				}
			}

			sb.WriteString("\"\n")
			sb.WriteString("            COMPREPLY=($(compgen -W \"${flags}\" -- ${cur}))\n")
			sb.WriteString("            return 0\n")
			sb.WriteString("            ;;\n")
		}
	}

	sb.WriteString("    esac\n")
	sb.WriteString("}\n\n")

	sb.WriteString(fmt.Sprintf("complete -F _%s_completion %s\n", cli.Name(), cli.Name()))

	return sb.String()
}

// zshCompletionTemplate generates a zsh completion script.
func zshCompletionTemplate(cli CLI) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("#compdef %s\n\n", cli.Name()))
	sb.WriteString(fmt.Sprintf("_%s() {\n", cli.Name()))
	sb.WriteString("    local -a commands\n")
	sb.WriteString("    commands=(\n")

	for _, cmd := range cli.Commands() {
		sb.WriteString(fmt.Sprintf("        '%s:%s'\n", cmd.Name(), cmd.Description()))
	}

	sb.WriteString("    )\n\n")
	sb.WriteString("    _arguments -C \\\n")
	sb.WriteString("        '1: :->command' \\\n")
	sb.WriteString("        '*::arg:->args'\n\n")

	sb.WriteString("    case $state in\n")
	sb.WriteString("        command)\n")
	sb.WriteString("            _describe 'command' commands\n")
	sb.WriteString("            ;;\n")
	sb.WriteString("        args)\n")
	sb.WriteString("            case $line[1] in\n")

	for _, cmd := range cli.Commands() {
		if len(cmd.Flags()) > 0 {
			sb.WriteString(fmt.Sprintf("                %s)\n", cmd.Name()))
			sb.WriteString("                    _arguments \\\n")

			for _, flag := range cmd.Flags() {
				shortOpt := ""
				if flag.ShortName() != "" {
					shortOpt = "'-" + flag.ShortName()
				}

				sb.WriteString(fmt.Sprintf("                        '%s[--%s]%s[%s]' \\\n",
					shortOpt, flag.Name(), shortOpt, flag.Description()))
			}

			sb.WriteString("                    ;;\n")
		}
	}

	sb.WriteString("            esac\n")
	sb.WriteString("            ;;\n")
	sb.WriteString("    esac\n")
	sb.WriteString("}\n\n")
	sb.WriteString(fmt.Sprintf("_%s\n", cli.Name()))

	return sb.String()
}

// fishCompletionTemplate generates a fish completion script.
func fishCompletionTemplate(cli CLI) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# fish completion for %s\n\n", cli.Name()))

	// Complete commands
	for _, cmd := range cli.Commands() {
		sb.WriteString(fmt.Sprintf("complete -c %s -f -n '__fish_use_subcommand' -a '%s' -d '%s'\n",
			cli.Name(), cmd.Name(), cmd.Description()))
	}

	sb.WriteString("\n")

	// Complete flags for each command
	for _, cmd := range cli.Commands() {
		for _, flag := range cmd.Flags() {
			shortOpt := ""
			if flag.ShortName() != "" {
				shortOpt = fmt.Sprintf("-s %s ", flag.ShortName())
			}

			sb.WriteString(fmt.Sprintf("complete -c %s -f -n '__fish_seen_subcommand_from %s' %s-l %s -d '%s'\n",
				cli.Name(), cmd.Name(), shortOpt, flag.Name(), flag.Description()))
		}
	}

	return sb.String()
}
