import { createContext, useContext, useEffect, useState, useCallback, useRef } from "react";
import type { ReactNode } from "react";
import type { TownConfig } from "../api/types";
import { configApi } from "../api/client";
import { deriveThemeVars, type ColorScheme } from "../lib/color";
import { useDocumentTitle } from "../hooks/useDocumentTitle";

interface ThemeContextValue {
  config: TownConfig;
  loading: boolean;
  updateConfig: (updates: Record<string, string>) => Promise<void>;
}

const defaults: TownConfig = {
  town_name: "The Bell",
};

const ThemeContext = createContext<ThemeContextValue>({
  config: defaults,
  loading: true,
  updateConfig: async () => {},
});

const DARK_QUERY = "(prefers-color-scheme: dark)";

/** Guarded because jsdom and older embedded browsers may not carry matchMedia. */
function darkMediaQuery(): MediaQueryList | null {
  return window.matchMedia?.(DARK_QUERY) ?? null;
}

function currentScheme(): ColorScheme {
  return darkMediaQuery()?.matches ? "dark" : "light";
}

function applyThemeColors(primary?: string, accent?: string, scheme: ColorScheme = "light") {
  const root = document.documentElement;
  for (const [name, value] of Object.entries(deriveThemeVars(primary, accent, scheme))) {
    root.style.setProperty(name, value);
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<TownConfig>(defaults);
  const [loading, setLoading] = useState(true);

  useDocumentTitle(config.town_name);

  // Read at apply time rather than held in state: nothing renders differently
  // for the scheme, so tracking it as state would re-render the whole tree to
  // change values the stylesheet reads directly.
  const colorsRef = useRef<Pick<TownConfig, "primary_color" | "accent_color">>({});

  useEffect(() => {
    configApi.getConfig()
      .then((cfg) => {
        setConfig({ ...defaults, ...cfg });
        colorsRef.current = { primary_color: cfg.primary_color, accent_color: cfg.accent_color };
        applyThemeColors(cfg.primary_color, cfg.accent_color, currentScheme());
      })
      .catch(() => {
        // Fall back to defaults silently
      })
      .finally(() => setLoading(false));
  }, []);

  // The town's colours are written as inline properties on the root element,
  // which outrank the stylesheet's own dark palette. So when the reader's
  // system flips scheme, these are the only colours that would not follow.
  useEffect(() => {
    const query = darkMediaQuery();
    if (!query) return;

    const handleChange = () => {
      const { primary_color, accent_color } = colorsRef.current;
      applyThemeColors(primary_color, accent_color, query.matches ? "dark" : "light");
    };

    query.addEventListener("change", handleChange);
    return () => query.removeEventListener("change", handleChange);
  }, []);

  const updateConfig = useCallback(async (updates: Record<string, string>) => {
    await configApi.updateConfig(updates);
    setConfig((prev) => {
      const next = { ...prev, ...updates };
      colorsRef.current = { primary_color: next.primary_color, accent_color: next.accent_color };
      applyThemeColors(next.primary_color, next.accent_color, currentScheme());
      return next;
    });
  }, []);

  return (
    <ThemeContext value={{ config, loading, updateConfig }}>
      {children}
    </ThemeContext>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext);
}
