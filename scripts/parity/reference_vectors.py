import json
import numpy as np
from sentence_transformers import SentenceTransformer

def main():
    model_id = "BAAI/bge-m3"
    
    print(f"Loading {model_id} via sentence-transformers...")
    model = SentenceTransformer(model_id)
    
    # Load sample texts
    json_path = "scripts/parity/sample_texts.json"
    with open(json_path, "r", encoding="utf-8") as f:
        sample_texts = json.load(f)
        
    print(f"Loaded {len(sample_texts)} sample texts from {json_path}.")
    
    # Encode
    print("Encoding sample texts with normalize_embeddings=True...")
    embeddings = model.encode(sample_texts, normalize_embeddings=True)
    
    # Save
    out_path = "scripts/parity/reference_vectors.npy"
    np.save(out_path, embeddings)
    
    print(f"Saved reference vectors to {out_path}")
    print(f"Embeddings shape: {embeddings.shape}")
    
    # Calculate norm of the first vector
    norm = np.linalg.norm(embeddings[0])
    print(f"First vector norm: {norm}")

if __name__ == "__main__":
    main()
