export function format(cents: number): string {
  const sign = cents < 0 ? "-" : "";
  return sign + (Math.abs(cents) / 100).toFixed(2);
}
export function parse(s: string): number {
  return Math.round(Number(s) * 100);
}
