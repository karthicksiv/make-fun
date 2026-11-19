// Dev tool (scratchpad only): the judge-view text reduction, sliced verbatim from the kit CLI
// (server/judging/dossier-view.ts) so I can preview what the judge is sent.
import { readFileSync, writeFileSync } from 'node:fs';
const DECL_MAX_BYTES = 96;
const HEAD_KEPT = /^(#{1,6}\s|(Source files|Files|Scripts|Instances|Totals):\s)/;
const utf8Bytes4 = (s3) => Buffer.byteLength(s3, "utf8");
const jsonBytes = (s3) => utf8Bytes4(JSON.stringify(s3)) - 2;
const plural8 = (n9, one, many = `${one}s`) => `${n9} ${n9 === 1 ? one : many}`;
const FENCE_OPEN2 = /^(`{3,}|~{3,})\s*([A-Za-z0-9_+.-]*)\s*$/;
const JS_KEYWORDS2 = new Set(["if","for","while","switch","catch","with","return","function","else","do","try","new","typeof","await","yield","super","delete","void","throw","in","of"]);
const vendored = (s3) => /(^|\/)vendor\//.test(s3.heading.slice(4).split(" (")[0] ?? "");
const linesCost = (lines2) => lines2.reduce((n9, l2) => n9 + jsonBytes(`${l2}\n`), 0);
function languageOf2(info, path85) {
  const tag = (info || (/\.([A-Za-z0-9]+)\)?\s*$/.exec(path85.split(" (")[0] ?? "")?.[1] ?? "")).toLowerCase();
  if ([
    "js",
    "mjs",
    "cjs",
    "jsx",
    "ts",
    "mts",
    "cts",
    "tsx",
    "javascript",
    "typescript"
  ].includes(tag))
    return "js";
  if (["lua", "luau"].includes(tag)) return "lua";
  if (["glsl", "vert", "frag", "wgsl"].includes(tag)) return "glsl";
  if (tag === "css") return "css";
  if (tag === "html" || tag === "htm") return "html";
  return "text";
}
function readKitText(text8) {
  const lines2 = text8.split("\n");
  const first = lines2.findIndex((l2) => l2.startsWith("### "));
  if (first < 0) return null;
  const head2 = lines2.slice(0, first).join("\n").replace(/\s+$/, "");
  const sections = [];
  const tail2 = [];
  let fences = 0;
  let current = null;
  let fenced = false;
  let fence = "";
  let sectionFenced = false;
  for (let i2 = first; i2 < lines2.length; i2++) {
    const line6 = lines2[i2];
    if (fenced) {
      if (line6.trimEnd().length >= fence.length && line6.trimEnd() === fence[0].repeat(line6.trimEnd().length)) {
        fenced = false;
        continue;
      }
      current.lines.push(line6);
      current.bytes += utf8Bytes4(line6) + 1;
      continue;
    }
    if (line6.startsWith("### ")) {
      current = {
        heading: line6.trimEnd(),
        facts: null,
        language: "text",
        fence: "```",
        info: "",
        lines: [],
        bytes: 0
      };
      sections.push(current);
      sectionFenced = false;
      continue;
    }
    const open11 = FENCE_OPEN2.exec(line6);
    if (open11 !== null && current !== null && !sectionFenced) {
      fenced = true;
      sectionFenced = true;
      fence = open11[1];
      fences++;
      current.fence = fence;
      current.info = open11[2];
      current.language = languageOf2(open11[2], current.heading.slice(4));
      continue;
    }
    if (line6.trim() === "") continue;
    if (current !== null && current.facts === null && !sectionFenced) {
      current.facts = line6.trim();
      continue;
    }
    if (/^\[\d+ of this file's \d+ lines? not shown\.\]$/.test(line6.trim()))
      continue;
    tail2.push(line6.trim());
  }
  return fences === 0 ? null : { head: head2, sections, tail: tail2 };
}
function jsDeclares(t2, top) {
  if (/^(export\s+(default\s+)?)?(async\s+)?function\b/.test(t2)) return true;
  if (/^(export\s+(default\s+)?)?(abstract\s+)?class\s+[\w$]/.test(t2))
    return true;
  if (/^export\b/.test(t2)) return true;
  if (/^(const|let|var)\s+[\w$]+\s*(:[^=]+)?=\s*(async\s+)?(function\b|\([^)]*\)\s*(:[^=]+)?=>|[\w$]+\s*=>)/.test(
    t2
  ))
    return true;
  if (/^[\w$]+\s*:\s*(async\s+)?(function\b|\([^)]*\)\s*=>)/.test(t2))
    return true;
  if (top && /^((const|let|var)\s|(interface|type|enum|declare)\s|[\w$]+(\.[\w$]+)+\s*=[^=])/.test(
    t2
  ))
    return true;
  const method = /^(static\s+)?(async\s+)?((get|set)\s+)?\*?#?([\w$]+)\s*\([^()]*\)\s*(:\s*[^{]+)?\{\s*\}?\s*$/.exec(
    t2
  );
  return method !== null && !JS_KEYWORDS2.has(method[5]);
}
function declarationsOf(language, lines2) {
  return declarationLines(language, lines2).map((d2) => d2.text);
}
function declarationLines(language, lines2) {
  const out = [];
  let block2 = null;
  const closers = {
    js: [["/*", "*/"]],
    glsl: [["/*", "*/"]],
    css: [["/*", "*/"]],
    html: [["<!--", "-->"]],
    lua: [["--[[", "]]"]]
  };
  for (let at3 = 0; at3 < lines2.length; at3++) {
    const raw2 = lines2[at3];
    const push4 = (text8) => {
      out.push({ line: at3, text: text8 });
    };
    let t2 = raw2.trim();
    if (block2 !== null) {
      const end = t2.indexOf(block2);
      if (end < 0) continue;
      t2 = t2.slice(end + block2.length).trim();
      block2 = null;
    }
    const opener = (closers[language] ?? []).find(
      ([open11]) => t2.startsWith(open11)
    );
    if (opener !== void 0) {
      const end = t2.indexOf(opener[1], opener[0].length);
      if (end < 0) {
        block2 = opener[1];
        continue;
      }
      t2 = t2.slice(end + opener[1].length).trim();
    }
    if (t2 === "") continue;
    const top = raw2.length > 0 && raw2[0] !== " " && raw2[0] !== "	";
    switch (language) {
      case "js":
        if (t2.startsWith("//") || t2.startsWith("*")) continue;
        if (jsDeclares(t2, top)) push4(t2);
        break;
      case "lua":
        if (t2.startsWith("--")) continue;
        if (/^(local\s+)?function\s+[\w.:]+\s*\(/.test(t2) || /^(local\s+)?[\w.:]+\s*=\s*function\s*\(/.test(t2))
          push4(t2);
        break;
      case "glsl":
        if (t2.startsWith("//")) continue;
        if (/^((void|float|double|int|uint|bool|[biud]?vec[234]|mat[234](x[234])?)\s+[A-Za-z_]\w*\s*\([^;]*$|fn\s+[A-Za-z_]\w*\s*\(|struct\s+\w+)/.test(
          t2
        ))
          push4(t2);
        break;
      case "css": {
        const brace = t2.indexOf("{");
        const selector = brace > 0 ? t2.slice(0, brace).trim() : "";
        if (selector !== "" && !selector.startsWith("}") && !/^[\w-]+\s*:\s/.test(selector))
          push4(selector);
        break;
      }
      case "html":
        if (/<[A-Za-z][^>]*\sid\s*=/.test(t2) || /<script\b/i.test(t2) || /<link\b/i.test(t2))
          push4(t2);
        break;
    }
  }
  return out;
}
function capped(line6) {
  if (utf8Bytes4(line6) <= DECL_MAX_BYTES)
    return { text: line6, shown: utf8Bytes4(line6) };
  let shown5 = 0;
  let text8 = "";
  for (const ch of line6) {
    const n9 = utf8Bytes4(ch);
    if (shown5 + n9 > DECL_MAX_BYTES - 3) break;
    text8 += ch;
    shown5 += n9;
  }
  return { text: `${text8.trimEnd()}…`, shown: shown5 };
}
function cutLine(budget, headings, decls, bytes2) {
  const what = headings > 0 ? `${plural8(headings, "file heading")}, ${plural8(decls, "declaration line")}` : plural8(decls, "declaration line");
  return `[Cut at ${budget} bytes: ${what} and ${bytes2} bytes of source left out.]`;
}
function firstLine15(form, pictures) {
  const what = form === "outline" ? "[An outline of the entry's source: each file's heading and its declarations, bodies and comments left out." : form === "bodies" ? "[An outline of the entry's source: each file's heading, then its text whole in file order as far as it fits, and past that its declarations only, bodies and comments left out." : form === "head" ? "[The start of the entry's text, cut to fit." : "[The entry's text, whole.";
  const { images, split: split3 } = pictures;
  const sheet = images === 1 ? " The image: its cover, scaled down.]" : !split3 ? ` The image: its ${images} screenshots on one sheet, the cover on top, the rest below in order.]` : images === 2 ? " The images: its cover, then its other screenshot.]" : ` The images: its cover, then its other ${images - 1} screenshots on one sheet, in order.]`;
  return what + sheet;
}
function fitLines(lines2, budget) {
  let used = 0;
  let k3 = 0;
  for (; k3 < lines2.length; k3++) {
    const cost = jsonBytes(`${lines2[k3]}
`);
    if (used + cost > budget) break;
    used += cost;
  }
  return k3;
}
function keptHead(head2) {
  const out = [];
  for (const line6 of head2.split("\n")) {
    if (line6.trim() === "") {
      if (out.length > 0 && out.at(-1) !== "") out.push("");
    } else if (HEAD_KEPT.test(line6)) out.push(line6.trimEnd());
  }
  while (out.at(-1) === "") out.pop();
  return out;
}
function viewText(text8, budget, images, split3 = false) {
  const pictures = { images, split: split3 };
  const whole = `${firstLine15("whole", pictures)}
${text8}`;
  if (jsonBytes(whole) <= budget)
    return { text: whole, form: "whole", decls: 0, bodies: 0 };
  const kit = readKitText(text8);
  if (kit === null) {
    const head3 = text8.split("\n").map((l2) => l2.trimEnd());
    const first = firstLine15("head", pictures);
    const room2 = budget - jsonBytes(`${first}
`) - jsonBytes(cutLine(budget, 0, 0, utf8Bytes4(text8)));
    const k3 = fitLines(head3, room2);
    const shown5 = head3.slice(0, k3);
    const left = utf8Bytes4(text8) - shown5.reduce((n9, l2) => n9 + utf8Bytes4(l2) + 1, 0);
    return {
      text: [first, ...shown5, cutLine(budget, 0, 0, Math.max(left, 0))].join(
        "\n"
      ),
      form: "head",
      decls: 0,
      bodies: 0
    };
  }
  const outlineFirst = firstLine15("outline", pictures);
  const head2 = keptHead(kit.head);
  const before = head2.length === 0 ? [] : [...head2, ""];
  const headings = kit.sections.map(
    (s3) => s3.facts === null ? s3.heading : `${s3.heading} (${s3.facts})`
  );
  const decls = kit.sections.map(
    (s3) => declarationLines(s3.language, s3.lines).map((d2) => ({
      line: d2.line,
      ...capped(d2.text)
    }))
  );
  const totalDecls = decls.reduce((n9, d2) => n9 + d2.length, 0);
  const sourceBytes = kit.sections.reduce((n9, s3) => n9 + s3.bytes, 0);
  const reserve = jsonBytes(
    cutLine(budget, kit.sections.length, totalDecls, sourceBytes)
  );
  const fixedCost = linesCost([
    outlineFirst,
    ...before,
    ...headings,
    ...kit.tail
  ]);
  if (fixedCost + reserve > budget) {
    const lines2 = [outlineFirst, ...before, ...headings, ...kit.tail];
    const k3 = fitLines(lines2, budget - reserve);
    const headingsShown = Math.min(
      Math.max(0, k3 - 1 - before.length),
      headings.length
    );
    return {
      text: [
        ...lines2.slice(0, k3),
        cutLine(
          budget,
          headings.length - headingsShown,
          totalDecls,
          sourceBytes
        )
      ].join("\n"),
      form: "outline",
      decls: 0,
      bodies: 0
    };
  }
  let room = budget - fixedCost - reserve;
  const kept = decls.map(() => 0);
  const open11 = decls.map((d2) => d2.length > 0);
  while (open11.some(Boolean)) {
    for (let f2 = 0; f2 < decls.length; f2++) {
      if (!open11[f2]) continue;
      const cost = jsonBytes(`${decls[f2][kept[f2]].text}
`);
      if (cost > room) {
        open11[f2] = false;
        continue;
      }
      room -= cost;
      kept[f2]++;
      if (kept[f2] === decls[f2].length) open11[f2] = false;
    }
  }
  const shownLines = kit.sections.map(() => null);
  let bodies = 0;
  const bodiesFirst = firstLine15("bodies", pictures);
  if (kept.every((k3, f2) => k3 === decls[f2].length)) {
    let left = room - (jsonBytes(`${bodiesFirst}
`) - jsonBytes(`${outlineFirst}
`));
    for (let f2 = 0; f2 < kit.sections.length; f2++) {
      const s3 = kit.sections[f2];
      if (vendored(s3)) continue;
      let cost = jsonBytes(`${s3.fence}${s3.info}
`) + jsonBytes(`${s3.fence}
`);
      let k3 = 0;
      let d2 = 0;
      const ds2 = decls[f2];
      for (; k3 < s3.lines.length; k3++) {
        let next2 = cost + jsonBytes(`${s3.lines[k3]}
`);
        let e8 = d2;
        while (e8 < ds2.length && ds2[e8].line === k3)
          next2 -= jsonBytes(`${ds2[e8++].text}
`);
        if (next2 > left) break;
        cost = next2;
        d2 = e8;
      }
      if (k3 === 0) break;
      shownLines[f2] = k3;
      bodies++;
      left -= cost;
      if (k3 < s3.lines.length) break;
    }
  }
  let shownDecls = 0;
  let shownBytes2 = 0;
  const body = kit.sections.flatMap((s3, f2) => {
    const k3 = shownLines[f2];
    if (shownLines[f2] === null) {
      const ds2 = decls[f2].slice(0, kept[f2]);
      shownDecls += ds2.length;
      shownBytes2 += ds2.reduce((n9, x2) => n9 + x2.shown, 0);
      return [headings[f2], ...ds2.map((x2) => x2.text)];
    }
    const rest = decls[f2].filter((x2) => x2.line >= k3);
    shownDecls += decls[f2].length;
    shownBytes2 += s3.lines.slice(0, k3).reduce((n9, l2) => n9 + utf8Bytes4(l2) + 1, 0) + rest.reduce((n9, x2) => n9 + x2.shown, 0);
    return [
      headings[f2],
      `${s3.fence}${s3.info}`,
      ...s3.lines.slice(0, k3),
      s3.fence,
      ...rest.map((x2) => x2.text)
    ];
  });
  const out = [
    bodies > 0 ? bodiesFirst : outlineFirst,
    ...before,
    ...body,
    ...kit.tail,
    cutLine(budget, 0, totalDecls - shownDecls, sourceBytes - shownBytes2)
  ];
  return { text: out.join("\n"), form: "outline", decls: shownDecls, bodies };
}
function familyImageTokens(family, w2, h2) {
  if (family === 'anthropic') return Math.min(Math.ceil(w2 / 28) * Math.ceil(h2 / 28), 4784);
  if (family === 'openai') {
    const patches = Math.ceil(Math.min(Math.ceil(w2 / 32) * Math.ceil(h2 / 32), 3e4) * 1.2);
    let s3 = Math.min(1, 2048 / Math.max(w2, h2));
    s3 *= Math.min(1, 768 / Math.min(w2 * s3, h2 * s3));
    const tiles = Math.ceil(Math.ceil(w2 * s3) / 512) * Math.ceil(Math.ceil(h2 * s3) / 512);
    return Math.max(patches, 85 + 170 * tiles);
  }
  return Math.max(258 * (w2 <= 384 && h2 <= 384 ? 1 : Math.ceil(w2 / 768) * Math.ceil(h2 / 768)), 1120);
}
const imageTokens = (w, h) => Math.max(...['anthropic', 'openai', 'google'].map((f) => familyImageTokens(f, w, h)));
const [dossierPath, outPath, sizes] = process.argv.slice(2);
const dossier = JSON.parse(readFileSync(dossierPath, 'utf8'));
// sizes: "1024x640,1024x640" = the cover and the sheet as the image step composed them
const dims = (sizes ?? '1024x640,1024x640').split(',').map((s) => s.split('x').map(Number));
const imgTok = dims.reduce((n, [w, h]) => n + imageTokens(w, h), 0);
const budget = (12288 - imgTok) * 3;
const v = viewText(dossier.text, budget, dossier.images.length, true);
writeFileSync(outPath, v.text);
console.log(`dossier text ${utf8Bytes4(dossier.text)} B (json ${jsonBytes(dossier.text)}) | image tokens ${imgTok} | text budget ${budget} B | view form=${v.form} decls=${v.decls} bodies=${v.bodies} out=${jsonBytes(v.text)} B`);
