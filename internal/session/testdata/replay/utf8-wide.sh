#!/bin/sh
# Wide characters at and near the right margin of an 80-column terminal.
a=$(printf '%079d' 0 | tr 0 a)
b=$(printf '%078d' 0 | tr 0 b)
c=$(printf '%077d' 0 | tr 0 c)
printf 'wide characters at the right margin (80 columns)\r\n'
printf '%s日x\r\n' "$a"
printf '%s日x\r\n' "$b"
printf '%s日本x\r\n' "$c"
printf '%s👍x\r\n' "$a"
printf '%s日本語x\r\n' "$b"
printf '日本語のテキスト、中文字符、한국어 텍스트 and ASCII mixed together on one long line that wraps\r\n'
sleep 3
