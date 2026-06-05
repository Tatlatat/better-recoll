import os
from optimum.onnxruntime import ORTModelForFeatureExtraction
from transformers import AutoTokenizer

def main():
    model_id = "BAAI/bge-m3"
    save_dir = "models/onnx/bge-m3"

    print(f"Loading and exporting {model_id} to ONNX...")
    model = ORTModelForFeatureExtraction.from_pretrained(model_id, export=True)
    tokenizer = AutoTokenizer.from_pretrained(model_id)

    print(f"Saving ONNX model and tokenizer to {save_dir}...")
    model.save_pretrained(save_dir)
    tokenizer.save_pretrained(save_dir)
    print("Export completed successfully.")

if __name__ == "__main__":
    main()
