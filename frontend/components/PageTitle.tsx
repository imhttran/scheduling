// Client pages can't export Next metadata, so each page renders this.
// React 19 hoists <title> into <head>, making it part of the tree: it
// prerenders into the static HTML and survives Next's hydration metadata
// commit — unlike a useEffect document.title write, which gets overwritten.
export function PageTitle({ title }: { title: string }) {
  return <title>{title}</title>;
}
