import * as React from "react";

import { useTheme } from "@/state/ThemeProvider";

/** Decorative floral flourishes tucked into the four corners of the workspace
 *  panel — a small girly touch that belongs only to the Blossom theme. The
 *  overlay fills its positioned parent (the <main> panel, so the flourishes
 *  frame the content rather than the whole window and never touch the nav rail
 *  or top bar), is non-interactive, and is hidden from assistive tech. It
 *  renders nothing at all on any other theme.
 *
 *  Everything is drawn from the blossom palette tokens (gtc-accent / -dim /
 *  -bright, gtc-title, the green project ramp), so it tracks the theme's own
 *  colours rather than hard-coding pinks. */

/** One rolled rose seen from above: soft petal backing plus a coiled centre. */
function Rose({ transform }: { transform?: string }) {
  return (
    <g transform={transform}>
      {[0, 72, 144, 216, 288].map((a) => (
        <path
          key={a}
          d="M0,0 C -5.5,-4 -5.5,-12 0,-14 C 5.5,-12 5.5,-4 0,0 Z"
          transform={`rotate(${a})`}
          fill="var(--gtc-accent-dim)"
          stroke="var(--gtc-accent)"
          strokeWidth="0.6"
          strokeOpacity="0.5"
        />
      ))}
      {/* Coiled centre: three shrinking arcs read as a rose's spiral. */}
      <path
        d="M -5,3 A 5,5 0 1,1 5,-1 A 3.2,3.2 0 1,1 -2.2,-2 A 1.5,1.5 0 1,1 1,-0.5"
        fill="none"
        stroke="var(--gtc-accent-bright)"
        strokeWidth="1.3"
        strokeLinecap="round"
      />
    </g>
  );
}

/** A five-petal blossom with a stamen cluster. */
function Blossom({ transform }: { transform?: string }) {
  return (
    <g transform={transform}>
      {[0, 72, 144, 216, 288].map((a) => (
        <path
          key={a}
          d="M0,0 C -4.5,-4.5 -3.6,-11 0,-10.5 C 3.6,-11 4.5,-4.5 0,0 Z"
          transform={`rotate(${a})`}
          fill="var(--gtc-panel)"
          stroke="var(--gtc-accent)"
          strokeWidth="0.7"
          strokeOpacity="0.55"
        />
      ))}
      <circle r="2.6" fill="var(--gtc-title)" />
      {[30, 150, 270].map((a) => (
        <circle key={a} r="0.8" cy="-3.4" fill="var(--gtc-accent-bright)" transform={`rotate(${a})`} />
      ))}
    </g>
  );
}

/** A closed bud: a teardrop petal cupped by two green sepals. */
function Bud({ transform }: { transform?: string }) {
  return (
    <g transform={transform}>
      <path d="M0,0 C -3.6,-4 -3.4,-9.5 0,-11 C 3.4,-9.5 3.6,-4 0,0 Z" fill="var(--gtc-accent-dim)" stroke="var(--gtc-accent)" strokeWidth="0.5" strokeOpacity="0.5" />
      <path d="M0,0 C -3,-3 -3,-6.5 -1.2,-8" fill="none" stroke="var(--dz-pj-green)" strokeWidth="1.2" strokeLinecap="round" />
      <path d="M0,0 C 3,-3 3,-6.5 1.2,-8" fill="none" stroke="var(--dz-pj-green)" strokeWidth="1.2" strokeLinecap="round" />
    </g>
  );
}

/** A single leaf with a centre vein. */
function Leaf({ transform }: { transform?: string }) {
  return (
    <g transform={transform}>
      <path
        d="M0,0 C 6,-5 15,-5 20,-1 C 15,3 6,5 0,0 Z"
        fill="var(--dz-pj-green)"
        fillOpacity="0.55"
        stroke="var(--dz-pj-green)"
        strokeWidth="0.6"
        strokeOpacity="0.6"
      />
      <path d="M1,0 C 7,-1 13,-1 18,-1" fill="none" stroke="var(--dz-pj-green)" strokeWidth="0.6" strokeOpacity="0.7" />
    </g>
  );
}

/** The corner spray, drawn once for the top-left corner (the cluster sits in
 *  the corner and two stems trail along the edges, wrapping it). The other
 *  three corners reuse this via a mirror transform. */
function CornerSpray() {
  return (
    <g>
      {/* Stems trailing along the two edges, like vines wrapping the corner. */}
      <path
        d="M6,150 C 30,120 40,80 60,58"
        fill="none"
        stroke="var(--dz-pj-green)"
        strokeWidth="1.6"
        strokeOpacity="0.55"
        strokeLinecap="round"
      />
      <path
        d="M150,6 C 120,28 92,40 66,52"
        fill="none"
        stroke="var(--dz-pj-green)"
        strokeWidth="1.6"
        strokeOpacity="0.55"
        strokeLinecap="round"
      />
      <path
        d="M10,12 C 30,34 44,40 58,50"
        fill="none"
        stroke="var(--dz-pj-green)"
        strokeWidth="1.4"
        strokeOpacity="0.5"
        strokeLinecap="round"
      />

      <Leaf transform="translate(26,110) rotate(115)" />
      <Leaf transform="translate(112,26) rotate(-10) scale(-1,1)" />
      <Leaf transform="translate(40,46) rotate(150) scale(0.8)" />

      <Bud transform="translate(22,34) rotate(-32) scale(0.9)" />

      <Blossom transform="translate(34,74) rotate(-18) scale(0.92)" />
      <Rose transform="translate(78,44) rotate(12) scale(1.12)" />
      <Rose transform="translate(64,86) rotate(-6) scale(0.78)" />
    </g>
  );
}

/** Non-interactive overlay filling the workspace panel: one small spray per
 *  corner. Must be rendered inside a positioned element (the <main> panel). */
export function BlossomCorners() {
  const { theme } = useTheme();
  if (theme !== "blossom") return null;

  const size = 50;
  const common: React.CSSProperties = {
    position: "absolute",
    width: size,
    height: size,
    opacity: 0.8,
  };
  // Each corner reuses the top-left spray, mirrored about the 160×160 viewBox
  // so the cluster always hugs its own corner (the translate re-homes the
  // flipped axis, which would otherwise land off-canvas).
  const corners: { style: React.CSSProperties; flip: string }[] = [
    { style: { ...common, top: 0, left: 0 }, flip: "" },
    { style: { ...common, top: 0, right: 0 }, flip: "translate(160,0) scale(-1,1)" },
    { style: { ...common, bottom: 0, left: 0 }, flip: "translate(0,160) scale(1,-1)" },
    { style: { ...common, bottom: 0, right: 0 }, flip: "translate(160,160) scale(-1,-1)" },
  ];

  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 z-[5] overflow-hidden">
      {corners.map((c, i) => (
        <svg key={i} viewBox="0 0 160 160" style={c.style}>
          <g transform={c.flip || undefined}>
            <CornerSpray />
          </g>
        </svg>
      ))}
    </div>
  );
}
