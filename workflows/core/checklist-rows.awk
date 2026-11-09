# Parse the first Status/Evidence table for the evidence and completion checks.
  function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
  function cells(line, out,    n, i) {
    gsub(/\\\|/, "\001", line)
    sub(/^[ \t]*\|/, "", line)
    sub(/\|[ \t]*$/, "", line)
    n = split(line, out, "|")
    for (i = 1; i <= n; i++) { gsub("\001", "|", out[i]); out[i] = trim(out[i]) }
    return n
  }
  done { next }
  !/^[ \t]*\|/ { if (in_table) done = 1; next }
  in_table && /^[ \t]*\|[ \t:|-]*$/ { next }
  in_table {
    rownum++
    cells($0, c)
    id = (id_col && c[id_col] != "") ? c[id_col] : "row " rownum
    status = tolower(c[status_col])
    gsub(/[`*_]/, "", status)
    gsub(/[ \t]+/, " ", status)
    status = trim(status)
    if (statuses_only) {
      if (status != "verified") printf "%s\t%s\n", id, status
      next
    }
    evidence = c[evidence_col]
    tokens = ""
    rest = evidence
    while (match(rest, /`[^`]+`/)) {
      token = substr(rest, RSTART + 1, RLENGTH - 2)
      rest = substr(rest, RSTART + RLENGTH)
      if (token !~ /(^|\/)\.\.(\/|$)/) tokens = tokens (tokens == "" ? "" : "\036") token
    }
    printf "%s\037%s\037%s\037%s\n", id, status, tokens, (evidence == "" ? "empty" : "text")
    next
  }
  {
    n = cells($0, h)
    status_col = evidence_col = id_col = 0
    for (i = 1; i <= n; i++) {
      name = tolower(h[i])
      if (name == "status") status_col = i
      else if (name == "evidence") evidence_col = i
      else if (name ~ /^id([^a-z0-9_-]|$)/) id_col = i
    }
    if (status_col && evidence_col) { in_table = 1; found = 1 }
  }
  END { if (!found && !statuses_only) print "NO_TABLE" }
