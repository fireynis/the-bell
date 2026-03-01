import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import type { KratosFlow } from "../api/kratos-types.ts";
import { createFlow, getFlow, submitFlow } from "../api/kratos.ts";

type FlowType = "login" | "registration" | "recovery" | "verification" | "settings";

interface FlowError {
  status?: number;
  body?: KratosFlow & { redirect_browser_to?: string; error?: { message?: string } };
}

export function useFlow(type: FlowType) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [flow, setFlow] = useState<KratosFlow | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const initFlow = useCallback(async () => {
    const flowId = searchParams.get("flow");
    try {
      if (flowId) {
        setFlow(await getFlow(type, flowId));
      } else {
        const f = await createFlow(type);
        setSearchParams({ flow: f.id }, { replace: true });
        setFlow(f);
      }
      setError(null);
    } catch (e: unknown) {
      const err = e as FlowError;
      // Already authenticated
      if (err.body?.redirect_browser_to) {
        window.location.href = err.body.redirect_browser_to;
        return;
      }
      // Flow expired or not found — create new one
      if (err.status === 410 || err.status === 404) {
        try {
          const f = await createFlow(type);
          setSearchParams({ flow: f.id }, { replace: true });
          setFlow(f);
          setError("Your session expired. Please try again.");
        } catch {
          setError("Failed to initialize. Please refresh the page.");
        }
        return;
      }
      setError("Something went wrong. Please try again.");
    }
  }, [type, searchParams, setSearchParams]);

  useEffect(() => {
    initFlow();
  }, [initFlow]);

  const submit = useCallback(
    async (values: Record<string, unknown>): Promise<{ success: boolean; flow?: KratosFlow }> => {
      if (!flow) return { success: false };
      setSubmitting(true);
      setError(null);
      try {
        const result = await submitFlow(type, flow.id, values);
        setFlow(result);
        return { success: true, flow: result };
      } catch (e: unknown) {
        const err = e as FlowError;
        // Validation errors — Kratos returns 400 with updated flow containing messages
        if (err.status === 400 && err.body?.ui) {
          setFlow(err.body as KratosFlow);
          return { success: false, flow: err.body as KratosFlow };
        }
        // Browser redirect (e.g., after successful login)
        if (err.status === 422 && err.body?.redirect_browser_to) {
          window.location.href = err.body.redirect_browser_to;
          return { success: false };
        }
        // Flow expired
        if (err.status === 410) {
          setError("Your session expired. Please try again.");
          await initFlow();
          return { success: false };
        }
        // Rate limited
        if (err.status === 429) {
          setError("Too many attempts. Please wait a moment and try again.");
          return { success: false };
        }
        setError("Something went wrong. Please try again.");
        return { success: false };
      } finally {
        setSubmitting(false);
      }
    },
    [flow, type, initFlow],
  );

  return { flow, error, submitting, submit, setFlow };
}
