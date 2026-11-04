# Condense one Claude Code transcript (.jsonl) to one line per event:
#
#   HH:MM:SS KIND text
#
# KIND is USER, SAID (assistant text), ERROR (a failed tool result), or the
# tool's name for a tool call, followed by its main argument. Successful
# tool results are dropped: they are most of a transcript's bytes, and the
# raw file still has them for checking a quote. Timestamps are UTC.
#
# Usage: jq -r -f digest.jq <session>.jsonl > digest.txt

def clip($n): tostring | gsub("\\s+"; " ") | if length > $n then .[0:$n] + "…" else . end;

(.timestamp // "" | .[11:19]) as $t
| if .type == "user" then
    ( .message.content
      | if type == "string" then "\($t) USER " + clip(2000)
        else .[]?
          | if .type == "text" then "\($t) USER " + (.text | clip(2000))
            elif .type == "tool_result" and .is_error == true then "\($t) ERROR " + (.content | clip(300))
            else empty end
        end )
  elif .type == "assistant" then
    ( .message.content[]?
      | if .type == "text" then "\($t) SAID " + (.text | clip(800))
        elif .type == "tool_use" then
          "\($t) \(.name) " + (
            if .name == "Agent" or .name == "Task" then
              "\(.input.subagent_type // "general-purpose") model=\(.input.model // "default") \(.input.description // "")"
            else
              (.input.command // .input.file_path // .input.skill // .input.description // .input.pattern // .input)
            end | clip(200))
        else empty end )
  else empty end
