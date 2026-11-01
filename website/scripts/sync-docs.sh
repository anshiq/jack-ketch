#!/bin/sh
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
src="$here/../docs"
ref="$here/content/docs/reference"
adr="$ref/decisions"

mkdir -p "$ref" "$adr"

emit() {
  file=$1
  out=$2
  title=$3
  desc=$4
  weight=$5
  {
    printf -- '---\ntitle: "%s"\ndescription: "%s"\nweight: %s\nsource: "%s"\n---\n\n' "$title" "$desc" "$weight" "$file"
    awk 'NR==1 && /^# /{next} {print}' "$src/$file" | sed '1{/^$/d}'
  } > "$out"
}

emit api.md "$ref/api.md" "Daemon API" "The jackketch.v1 Connect-style RPC services served by jackketchd." 40
emit architecture.md "$ref/architecture.md" "Architecture" "Components, storage layout, API services and performance budgets." 50
emit security.md "$ref/security.md" "Security model" "Threats, mitigations, audit and how credential profiles are protected." 60
emit troubleshooting.md "$ref/troubleshooting.md" "Troubleshooting" "Every doctor check mapped to an action, plus common issues." 70
emit migration-v3.md "$ref/migrate-v3.md" "Migrating to v3" "How legacy v2 state moves into the central store." 80
emit integrations.md "$here/content/docs/integrations.md" "Agent integrations" "Per-agent config paths, the exact entry written, restart hints and caveats for every supported client." 30

n=10
for f in "$src"/adr/*.md; do
  base=$(basename "$f" .md)
  title=$(sed -n '1s/^# //p' "$f")
  {
    printf -- '---\ntitle: "%s"\nweight: %s\nsource: "adr/%s.md"\n---\n\n' "$title" "$n" "$base.md"
    awk 'NR==1 && /^# /{next} {print}' "$f"
  } > "$adr/$base.md"
  n=$((n + 10))
done

sed -i 's/^Accepted$/Superseded. The desktop GUI was removed; the web UI is the only interface./' "$adr/005-wails-gui.md"
