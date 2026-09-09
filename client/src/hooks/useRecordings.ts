import { useQuery } from "@tanstack/react-query";
import { listRecordings } from "@/services/sessions";

export const useRecordings = (sid: string | null, enabled: boolean) =>
  useQuery({
    queryKey: ["recordings", sid],
    queryFn: () => listRecordings(sid as string),
    enabled: enabled && !!sid,
    refetchInterval: enabled ? 5000 : false,
  });
