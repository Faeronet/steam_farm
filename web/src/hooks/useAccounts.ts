import { useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, type Account } from '@/api/client';
import { useWebSocket } from './useWebSocket';

export function useAccounts(gameType?: string) {
  const qc = useQueryClient();
  const { subscribe } = useWebSocket();

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === 'farm:status') {
        qc.invalidateQueries({ queryKey: ['accounts', gameType] });
      }
    });
  }, [subscribe, qc, gameType]);

  return useQuery({
    queryKey: ['accounts', gameType],
    queryFn: () => api.getAccounts(gameType),
    refetchInterval: 10_000,
  });
}

export function useCreateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: Partial<Account> & { password: string }) => api.createAccount(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['accounts'] }),
  });
}

export function useDeleteAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteAccount(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['accounts'] }),
  });
}
