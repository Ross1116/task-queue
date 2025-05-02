const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

/**
 * Submits a new task to the backend API.
 * @param {string} input - The input data for the task.
 * @returns {Promise<{ task_id: string }>} - A promise resolving to the created task ID.
 * @throws {Error} - Throws an error if the API request fails.
 */
export async function submitTask(input: string): Promise<{ task_id: string }> {
	try {
		const response = await fetch(`${API_BASE_URL}/tasks`, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({ input }),
		});

		if (!response.ok) {
			const errorData = await response.json().catch(() => ({ error: 'Failed to submit task with status: ' + response.status }));
			throw new Error(errorData.error || `HTTP error! status: ${response.status}`);
		}

		const data = await response.json();
		if (!data.task_id) {
			throw new Error('Task ID not found in response');
		}
		return data;
	} catch (error) {
		console.error("Error submitting task:", error);
		throw error;
	}
}

// Define a type for the expected Task Status structure from the API
export interface TaskStatus {
	task_id: string;
	status: 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED' | string;
	input?: string;
	result?: {
		String: string;
		Valid: boolean;
	} | null;
	created_at: string;
	updated_at: string;
}


/**
 * Fetches the status of a specific task from the backend API.
 * @param {string} taskId - The ID of the task to check.
 * @returns {Promise<TaskStatus>} - A promise resolving to the task status details.
 * @throws {Error} - Throws an error if the API request fails or task is not found.
 */
export async function getTaskStatus(taskId: string): Promise<TaskStatus> {
	if (!taskId) {
		throw new Error("Task ID is required to fetch status.");
	}
	try {
		const response = await fetch(`${API_BASE_URL}/tasks/${taskId}/status`);

		if (!response.ok) {

			if (response.status === 404) {
				throw new Error('Task not found');
			}
			const errorData = await response.json().catch(() => ({ error: 'Failed to get task status with code: ' + response.status }));
			throw new Error(errorData.error || `HTTP error! status: ${response.status}`);
		}

		const data: TaskStatus = await response.json();
		return data;
	} catch (error) {
		console.error(`Error fetching status for task ${taskId}:`, error);
		throw error;
	}
}

