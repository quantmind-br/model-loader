package benchmark

import (
	"fmt"
	"sort"
	"strings"
)

// This mirrors SWE-bench's inference make_datasets prompt_style_3 (the "oracle"
// retrieval setting): the model is given the full source of the file(s) the
// gold patch touches, plus the issue, and asked to emit a git-apply-able patch.

const swePremise = "You will be provided with a partial code base and an issue statement explaining a problem to resolve."

const swePatchExample = `--- a/file.py
+++ b/file.py
@@ -1,27 +1,35 @@
 def euclidean(a, b):
-    while b:
-        a, b = b, a % b
-    return a
+    if b == 0:
+        return a
+    return euclidean(b, a % b)


 def bresenham(x0, y0, x1, y1):
     points = []
     dx = abs(x1 - x0)
     dy = abs(y1 - y0)
-    sx = 1 if x0 < x1 else -1
-    sy = 1 if y0 < y1 else -1
-    err = dx - dy
+    x, y = x0, y0
+    sx = -1 if x0 > x1 else 1
+    sy = -1 if y0 > y1 else 1

-    while True:
-        points.append((x0, y0))
-        if x0 == x1 and y0 == y1:
-            break
-        e2 = 2 * err
-        if e2 > -dy:
+    if dx > dy:
+        err = dx / 2.0
+        while x != x1:
+            points.append((x, y))
             err -= dy
-            x0 += sx
-        if e2 < dx:
-            err += dx
-            y0 += sy
+            if err < 0:
+                y += sy
+                err += dx
+            x += sx
+    else:
+        err = dy / 2.0
+        while y != y1:
+            points.append((x, y))
+            err -= dx
+            if err < 0:
+                x += sx
+                err += dy
+            y += sy

+    points.append((x, y))
     return points`

const sweExampleExplanation = "Here is an example of a patch file. It consists of changes to the code base. " +
	"It specifies the file names, the line numbers of each change, and the removed and added lines. " +
	"A single patch file can contain changes to multiple files."

const sweFinalInstruction = "I need you to solve the provided issue by generating a single patch file that I can apply " +
	"directly to this repository using git apply. Please respond with a single patch file in the format shown above."

// BuildPrompt assembles the SWE-bench oracle (prompt_style_3) messages.
func BuildPrompt(p Problem) []ChatMessage {
	var b strings.Builder
	b.WriteString(swePremise)
	b.WriteString("\n<issue>\n")
	b.WriteString(p.Statement)
	b.WriteString("\n</issue>\n\n<code>\n")
	b.WriteString(makeCodeText(p.ContextFiles))
	b.WriteString("\n</code>\n\n")
	b.WriteString(sweExampleExplanation)
	b.WriteString("\n<patch>\n")
	b.WriteString(swePatchExample)
	b.WriteString("\n</patch>\n\n")
	b.WriteString(sweFinalInstruction)
	b.WriteString("\nRespond below:")
	return []ChatMessage{{Role: "user", Content: b.String()}}
}

// makeCodeText renders each oracle file as [start of {path}] + line-numbered
// content + [end of {path}], matching SWE-bench's make_code_text.
func makeCodeText(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&b, "[start of %s]\n", path)
		lines := strings.Split(files[path], "\n")
		for i, line := range lines {
			fmt.Fprintf(&b, "%d %s\n", i+1, line)
		}
		fmt.Fprintf(&b, "[end of %s]\n", path)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExtractDiff pulls a unified diff out of the model's reply. SWE-bench prompts
// ask for a raw patch (no fence), so the bare-diff fallback is the common path;
// fenced ```diff blocks are also accepted.
func ExtractDiff(content string) (string, bool) {
	if d, ok := fencedBlock(content, "diff"); ok {
		return strings.TrimSpace(d) + "\n", true
	}
	for _, lang := range []string{"patch", "", "text"} {
		if d, ok := fencedBlock(content, lang); ok && looksLikeDiff(d) {
			return strings.TrimSpace(d) + "\n", true
		}
	}
	if idx := diffStart(content); idx >= 0 {
		return strings.TrimSpace(content[idx:]) + "\n", true
	}
	return "", false
}

// fencedBlock returns the body of the first ```<lang> fenced block. When lang
// is "" it matches a bare ``` fence.
func fencedBlock(content, lang string) (string, bool) {
	open := "```" + lang
	lines := strings.Split(content, "\n")
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i])
		if lang == "" {
			if trimmed != "```" {
				continue
			}
		} else if trimmed != open {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				return strings.Join(lines[i+1:j], "\n"), true
			}
		}
	}
	return "", false
}

func looksLikeDiff(s string) bool {
	return strings.Contains(s, "diff --git") ||
		(strings.Contains(s, "--- ") && strings.Contains(s, "+++ "))
}

func diffStart(content string) int {
	for _, marker := range []string{"diff --git ", "--- a/"} {
		if idx := strings.Index(content, marker); idx >= 0 {
			return idx
		}
	}
	return -1
}
