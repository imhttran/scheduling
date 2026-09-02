// Brand mark: HTT Tiers — the tree built from the initials, duet colorway.
// The H crown and T tier are always the brand burnt orange; the ground tier
// and trunk use .logo-base, which globals.css flips to a light navy in dark
// mode so it never disappears on navy surfaces.
export function Logo({ size = 40 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 96 96"
      role="img"
      aria-label="HTT logo"
      className="logo"
    >
      <g className="logo-canopy" fill="currentColor">
        <rect x="28" y="10" width="7" height="24" />
        <rect x="61" y="10" width="7" height="24" />
        <rect x="28" y="18.5" width="40" height="7" />
        <rect x="16" y="40" width="64" height="7" />
        <rect x="44.5" y="47" width="7" height="11" />
      </g>
      <g className="logo-base" fill="currentColor">
        <rect x="8" y="64" width="80" height="7" />
        <rect x="44.5" y="71" width="7" height="17" />
      </g>
    </svg>
  );
}
