export interface TimeRange {
  from: Date;
  to: Date;
}

export const rangeTypes = ["httplog", "slowlog", "pprof", "memo"];
export const rangeTypeLabels: { [key: string]: string } = {
  httplog: "ALP (HTTP Log)",
  slowlog: "Slow Query",
  pprof: "pprof",
  memo: "Memo",
};

const MINUTE = 60 * 1000;

const pad = (n: number) => String(n).padStart(2, "0");

export const toInputValue = (d: Date) =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;

export const fromInputValue = (v: string): Date | undefined => {
  const d = new Date(v);
  return isNaN(d.getTime()) ? undefined : d;
};

// pprof and the tail handler both measure forward from Datetime; memo has no
// Duration and is a single point in time
export const intervalOf = (
  datetime: Date,
  duration: number,
): [number, number] => {
  const start = datetime.getTime();
  return [start, start + (duration || 0) * 1000];
};

// overlap rather than containment, so a run straddling a bound still counts
export const overlaps = (
  datetime: Date,
  duration: number,
  range: TimeRange,
) => {
  const [start, end] = intervalOf(datetime, duration);
  return start <= range.to.getTime() && end >= range.from.getTime();
};

interface Measured {
  Type: string;
  Datetime: Date;
  Duration: number;
}

// memos are often attached long after a run, so they only define the span of
// a collection that holds nothing else
export const measuredSpan = (snapshots: Measured[]): TimeRange | undefined => {
  const measurements = snapshots.filter((s) => s.Type != "memo");
  const basis = measurements.length ? measurements : snapshots;
  if (!basis.length) {
    return undefined;
  }

  const bounds = basis.map((s) => intervalOf(s.Datetime, s.Duration));
  return {
    from: new Date(Math.min(...bounds.map(([start]) => start))),
    to: new Date(Math.max(...bounds.map(([, end]) => end))),
  };
};

// a time alone is ambiguous once a range reaches over midnight
export const spansDays = (range: TimeRange) =>
  range.from.toDateString() != range.to.toDateString();

const spanOf = (range: TimeRange) => range.to.getTime() - range.from.getTime();

export const canShrink = (range: TimeRange) => spanOf(range) > MINUTE;

export const widened = (range: TimeRange, minutes: number): TimeRange => {
  const from = range.from.getTime() - minutes * MINUTE;
  const to = range.to.getTime() + minutes * MINUTE;
  if (to - from >= MINUTE) {
    return { from: new Date(from), to: new Date(to) };
  }

  // the URL cannot express less than a minute, so settle on the one in the middle
  const middle = range.from.getTime() + spanOf(range) / 2;
  const minute = Math.floor(middle / MINUTE) * MINUTE;
  return { from: new Date(minute), to: new Date(minute + MINUTE) };
};

// editing one bound past the other moves the whole window instead of being ignored
export const withFrom = (range: TimeRange, from: Date): TimeRange =>
  from <= range.to
    ? { from, to: range.to }
    : { from, to: new Date(from.getTime() + spanOf(range)) };

export const withTo = (range: TimeRange, to: Date): TimeRange =>
  range.from <= to
    ? { from: range.from, to }
    : { from: new Date(to.getTime() - spanOf(range)), to };

type QueryValue = string | null | (string | null)[];

const firstValue = (v: QueryValue | undefined): string => {
  if (typeof v == "string") {
    return v;
  }
  if (Array.isArray(v) && typeof v[0] == "string") {
    return v[0];
  }
  return "";
};

export const rangeFromQuery = (query: {
  [key: string]: QueryValue;
}): TimeRange | undefined => {
  const from = fromInputValue(firstValue(query.from));
  const to = fromInputValue(firstValue(query.to));
  return from && to && from <= to ? { from, to } : undefined;
};

// round the end up: truncating it would cut off the run a range was seeded from
export const rangeToQuery = (range: TimeRange) => ({
  from: toInputValue(range.from),
  to: toInputValue(new Date(Math.ceil(range.to.getTime() / MINUTE) * MINUTE)),
});
