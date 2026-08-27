import { useMutation } from "@tanstack/react-query";
import { setHoldCall } from "@/services/calls";

export const useHoldCall = () =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string; hold: boolean }) => {
      await setHoldCall(vars.sid, vars.callId, vars.hold);
    },
  });
