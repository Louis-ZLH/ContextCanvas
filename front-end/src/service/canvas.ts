import type { JSONResponse } from "./type";
import { apiRequest, ApiError } from "../util/api";
import type { CanvasListInfo } from "./type";
import { toCamelCase } from "../util/transform";
import type { Canvas, CanvasDetail, syncResponse, fullSyncResponse, CanvasSearchResponse } from "./type";
import type { GraphDelta } from "../feature/canvas/types";
import type { Node, Edge } from "../feature/canvas/types";
import type { getCanvasVersionResponse, BackendNode, DTONodeReadyToSend } from "./type";
import type { Conversation } from "../feature/chat/types";

export async function createCanvas(): Promise<{ success: boolean, message: string, data: Canvas | null }> {
    try{
        const response = await apiRequest<JSONResponse>("/api/canvas/create", {
            method: "POST",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }

        const canvas = toCamelCase(response.data) as unknown as Canvas;
        return { success: true, message: response.message, data: canvas };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to create canvas", data: null };
        }
        return { success: false, message: "Failed to create canvas", data: null };
    }
}

export async function getCanvasList(): Promise<{ success: boolean, message: string, data: CanvasListInfo | null }> {
    try{
        const response = await apiRequest<JSONResponse>("/api/canvas/list", {
            method: "GET",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        
        // Use generic utility to convert all keys to camelCase
        const canvasListInfo = toCamelCase(response.data) as CanvasListInfo;

        return { success: true, message: response.message, data: canvasListInfo };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to get canvas list", data: null };
        }
        return { success: false, message: "Failed to get canvas list", data: null };
    }
}

export async function deleteCanvas(canvasId: string): Promise<{ success: boolean, message: string, data: null }> {
    try{
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}`, {
            method: "DELETE",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        return { success: true, message: response.message, data: null };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to delete canvas", data: null };
        }
        return { success: false, message: "Failed to delete canvas", data: null };
    }
}

export async function renameCanvas(canvasId: string, title: string): Promise<{ success: boolean, message: string, data: null }> {
    try{
        const response = await apiRequest<JSONResponse>(`/api/canvas/rename/${canvasId}`, {
            method: "PATCH",
            body: JSON.stringify({ title }),
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        return { success: true, message: response.message, data: null };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to rename canvas", data: null };
        }
        return { success: false, message: "Failed to rename canvas", data: null };
    }
}

export async function getCanvasDetail(canvasId: string): Promise<{ success: boolean, message: string, data: CanvasDetail | null }> {
    try{
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}`, {
            method: "GET",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        return { success: true, message: response.message, data: toCamelCase(response.data) as CanvasDetail };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to get canvas detail", data: null };
        }
        return { success: false, message: "Failed to get canvas detail", data: null };
    }
}

export async function syncCanvas(canvasId: string, delta: GraphDelta, version: number): Promise<{ success: boolean, message: string, data: syncResponse | null, code: number }> {
    try{
        const deltaToSend = {
            updatedNodes: delta.updatedNodes.map(convertNodeToSendStructure),
            createdNodes: delta.createdNodes.map(convertNodeToSendStructure),
            deletedNodesId: delta.deletedNodesId,
            createdEdges: delta.createdEdges,
            deletedEdges: delta.deletedEdges,
        };
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}/sync`, {
            method: "POST",
            body: JSON.stringify({ ...deltaToSend, clientVersion:version }),
        });

        if(response.code !== 0) {
            return { success: false, message: response.message, data: null, code: 0 };
        }
        return { success: true, message: response.message, data: toCamelCase(response.data) as syncResponse, code: 0 };
    } catch (error: unknown) {
        if (error instanceof ApiError) {
            return { success: false, message: error.message || "Failed to sync canvas", data: null, code: error.status };
        }
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to sync canvas", data: null, code: 0 };
        }
        return { success: false, message: "Failed to sync canvas", data: null, code: 0 };
    }
}

export async function fullSyncCanvas(
    canvasId: string, 
    nodes: Node[], 
    edges: Edge[], 
    version: number
): Promise<{ success: boolean, message: string, data: fullSyncResponse | null, code: number }> {
    try{
        const nodesToSend = nodes.map(convertNodeToSendStructure);
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}/full-sync`, {
            method: "POST",
            body: JSON.stringify({ canvas_id: canvasId, nodes: nodesToSend, edges, clientVersion:version }),
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null, code: 0 };
        }
        return { success: true, message: response.message, data: toCamelCase(response.data) as fullSyncResponse, code: 0 };
    } catch (error: unknown) {
        if (error instanceof ApiError) {
            return { success: false, message: error.message || "Failed to full sync canvas", data: null, code: error.status };
        }
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to full sync canvas", data: null, code: 0 };
        }
        return { success: false, message: "Failed to full sync canvas", data: null, code: 0 };
    }
}

export async function getCanvasVersion(canvasId: string): Promise<{ success: boolean, message: string, data: getCanvasVersionResponse | null}> {
    try{
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}/version`, {
            method: "GET",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null};
        }
        return { success: true, message: response.message, data: toCamelCase(response.data) as getCanvasVersionResponse };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to get canvas version", data: null };
        }
        return { success: false, message: "Failed to get canvas version", data: null };
    }
}

export function convertNodeToSendStructure(node: Node): DTONodeReadyToSend {
    const fileId = node.data.fileId;
    return {
        id: node.id,
        type: node.type,
        position: node.position,
        file_id: fileId && fileId !== "__error__" ? fileId : undefined,
    };
}

export function convertBackendNodeToNode(node: BackendNode): Node {
    return {
        id: node.id,
        type: node.type,
        position: node.position,
        data: {
            fileId: node.fileId,
        },
    };
}

export async function searchCanvases(params: {
    keyword: string;
    page: number;
    limit: number;
}): Promise<{ success: boolean; message: string; data: CanvasSearchResponse | null }> {
    try {
        const query = new URLSearchParams({
            keyword: params.keyword,
            page: String(params.page),
            limit: String(params.limit),
        });
        const response = await apiRequest<JSONResponse>(`/api/canvas-search?${query}`, {
            method: "GET",
        });
        if (response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        return { success: true, message: response.message, data: toCamelCase(response.data) as CanvasSearchResponse };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message, data: null };
        }
        return { success: false, message: "Failed to search canvases", data: null };
    }
}

export async function getConversationList(canvasId: string): Promise<{ success: boolean, message: string, data: Conversation[] | null }> {
    try{
        const response = await apiRequest<JSONResponse>(`/api/canvas/${canvasId}/conversation/list`, {
            method: "GET",
        });
        if(response.code !== 0) {
            return { success: false, message: response.message, data: null };
        }
        return { success: true, message: response.message, data: toCamelCase(response.data?.conversations) as Conversation[] };
    } catch (error: unknown) {
        if (error instanceof Error) {
            return { success: false, message: error.message || "Failed to get conversation list", data: null };
        }
        return { success: false, message: "Failed to get conversation list", data: null };
    }
}