import { Card, CardContent } from "@/components/ui/card";

export function Stat({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail: string;
}) {
  return (
    <Card className="group relative overflow-hidden transition-[border-color,transform,box-shadow] duration-200 hover:-translate-y-0.5 hover:border-primary/30 hover:shadow-[var(--shadow-md)]">
      <div aria-hidden className="absolute inset-y-0 left-0 w-1 bg-primary transition-colors group-hover:bg-signal" />
      <CardContent className="pl-6">
        <p className="text-[11px] font-bold uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
        <p className="data-value mt-2 font-[Arial_Narrow] text-3xl font-bold tracking-tight text-navy">{value}</p>
        <p className="mt-1 text-sm text-muted-foreground">{detail}</p>
      </CardContent>
    </Card>
  );
}
