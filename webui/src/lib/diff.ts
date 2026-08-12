// Small LCS-based line diff — enough for comparing two Recipe Spec JSON
// blobs (a handful to a few hundred lines), no dependency needed.
export interface DiffOp {
  t: "eq" | "add" | "del"
  line: string
}

export function diffLines(aText: string, bText: string): DiffOp[] {
  const a = aText.split("\n")
  const b = bText.split("\n")
  const n = a.length
  const m = b.length
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))

  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }

  const out: DiffOp[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ t: "eq", line: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ t: "del", line: a[i] })
      i++
    } else {
      out.push({ t: "add", line: b[j] })
      j++
    }
  }
  while (i < n) {
    out.push({ t: "del", line: a[i] })
    i++
  }
  while (j < m) {
    out.push({ t: "add", line: b[j] })
    j++
  }
  return out
}
