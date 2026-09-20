// Types and helpers for the diagnosis / diff API exposed by internal/diag.

export type Category =
  | "INDEX"
  | "N+1"
  | "CACHE"
  | "APP-CPU"
  | "STATIC"
  | "LOCK"
  | "ERROR"
  | "PAYLOAD";

export type Severity = "error" | "warn" | "info";
export type DiffDirection = "improved" | "worsened" | "neutral";
export type DiffStatus = "both" | "added" | "gone";

export interface Evidence {
  Label: string;
  Value: string;
  Ratio?: number;
}

export interface Finding {
  Category: Category;
  Title: string;
  Subject: string;
  ImpactShare: number;
  Confidence: number;
  Effort: number;
  Score: number;
  Evidence: Evidence[];
  Suggestion: string;
  Source: string;
}

export interface HealthIssue {
  Severity: Severity;
  Source: string;
  Title: string;
  Detail: string;
  Hint: string;
}

export interface Summary {
  TotalRequests: number;
  TotalAppTime: number;
  TotalQueries: number;
  TotalQueryTime: number;
  HasHTTPLog: boolean;
  HasSlowLog: boolean;
  HasPprof: boolean;
}

export interface Report {
  GroupID: string;
  Health: HealthIssue[];
  Findings: Finding[];
  Summary: Summary;
}

export interface GroupInfo {
  GroupID: string;
  Datetime: string;
  HasHTTPLog: boolean;
  HasSlowLog: boolean;
  HasPprof: boolean;
}

export interface HTTPDiff {
  Endpoint: string;
  CountBefore: number;
  CountAfter: number;
  CountDelta: number;
  SumBefore: number;
  SumAfter: number;
  SumDelta: number;
  AvgBefore: number;
  AvgAfter: number;
  AvgDelta: number;
  AvgPct: number;
  ErrBefore: number;
  ErrAfter: number;
  Status: DiffStatus;
  Direction: DiffDirection;
}

export interface QueryDiff {
  Query: string;
  CountBefore: number;
  CountAfter: number;
  CountDelta: number;
  SumBefore: number;
  SumAfter: number;
  SumDelta: number;
  SumPct: number;
  ExaminedBefore: number;
  ExaminedAfter: number;
  Status: DiffStatus;
  Direction: DiffDirection;
}

export interface DiffTotals {
  RequestsBefore: number;
  RequestsAfter: number;
  AppTimeBefore: number;
  AppTimeAfter: number;
  AppTimePct: number;
  QueriesBefore: number;
  QueriesAfter: number;
  QueryTimeBefore: number;
  QueryTimeAfter: number;
  QueryTimePct: number;
  ErrorsBefore: number;
  ErrorsAfter: number;
}

export interface DiffReport {
  BeforeGroup: string;
  AfterGroup: string;
  HTTP: HTTPDiff[];
  Query: QueryDiff[];
  Totals: DiffTotals;
}

const request = async <T>(path: string): Promise<T | null> => {
  try {
    const resp = await fetch(path);
    if (!resp.ok) {
      alert(`http error: status=${resp.status}, message=${await resp.text()}`);
      return null;
    }
    return (await resp.json()) as T;
  } catch (e) {
    alert(e);
    return null;
  }
};

export const fetchGroups = () => request<GroupInfo[]>("/api/diag/groups");

export const fetchReport = (gid: string) =>
  request<Report>(`/api/diag/report/${encodeURIComponent(gid)}`);

export const fetchDiff = (before: string, after: string) =>
  request<DiffReport>(
    `/api/diag/diff?before=${encodeURIComponent(
      before,
    )}&after=${encodeURIComponent(after)}`,
  );

// Effort is scored numerically on the server; these are the labels used in the UI.
export const effortLabel = (effort: number): string => {
  if (effort <= 1) return "極小";
  if (effort <= 2) return "小";
  if (effort <= 3) return "中";
  if (effort <= 5) return "大";
  return "特大";
};

export const confidenceLabel = (c: number): string => {
  if (c >= 0.85) return "高";
  if (c >= 0.7) return "中";
  return "低";
};

// Symbols accompany every colour-coded value so that state is never conveyed by
// colour alone.
export const directionSymbol = (d: DiffDirection): string => {
  switch (d) {
    case "improved":
      return "▼ 改善";
    case "worsened":
      return "▲ 悪化";
    default:
      return "— 横ばい";
  }
};

export const severitySymbol = (s: Severity): string => {
  switch (s) {
    case "error":
      return "✕";
    case "warn":
      return "!";
    default:
      return "i";
  }
};

export const formatPct = (v: number): string =>
  `${v > 0 ? "+" : ""}${v.toFixed(1)}%`;

export const formatSec = (v: number): string => {
  if (v >= 1) return `${v.toFixed(2)}s`;
  return `${(v * 1000).toFixed(0)}ms`;
};

export const formatInt = (v: number): string => v.toLocaleString("en-US");
