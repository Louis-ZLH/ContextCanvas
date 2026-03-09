import { queryOptions } from "@tanstack/react-query";
import { getCanvasList, searchCanvases } from "../service/canvas";

export const canvasListQueryOptions = queryOptions({
    queryKey: ["canvas", "list"],
    queryFn: getCanvasList,
    staleTime: 1000 * 60 * 5, // 5分钟内认为数据是新鲜的，中间件不会重复请求
    retry: false, // 鉴权失败不要重试，直接跳登录
  });

export function searchCanvasQueryOptions(params: { keyword: string; page: number; limit: number }) {
    return queryOptions({
        queryKey: ["canvas", "search", params],
        queryFn: () => searchCanvases(params),
        staleTime: 2 * 60 * 1000,
        enabled: params.keyword.length > 0,
        retry: false,
    });
}