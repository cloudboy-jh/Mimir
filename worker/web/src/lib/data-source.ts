export function fixturesAllowed(source: string | undefined, development: boolean, mode: string) {
  return source === "fixtures" && (development || mode === "demo");
}

export const fixtureDataEnabled = fixturesAllowed(
  import.meta.env.VITE_MIMIR_DATA_SOURCE,
  import.meta.env.DEV,
  import.meta.env.MODE,
);

export const demoMode = import.meta.env.MODE === "demo";
